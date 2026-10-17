package operations

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"infinite-canvas/backend/internal/database"
)

func directorNode(id string) map[string]any {
	return map[string]any{
		"id": id, "type": "director", "title": "导演台",
		"metadata": map[string]any{"workflowKind": "shot", "directorSceneId": "scene-1"},
	}
}

func TestDirectorPreviewProposeAndConfirm(t *testing.T) {
	ResetDirectorDispatchHub()
	t.Cleanup(ResetDirectorDispatchHub)
	probe := &proposeProbe{doc: proposeCanvas(t, []map[string]any{directorNode("dir-1")})}
	db := directorPreviewDB(t)
	registry := NewRegistry(binderFunc(func(*gorm.DB) Domain { return probe }), NewStore(db))
	RegisterDefaultOps(registry)

	proposed, err := registry.Execute(Request{
		Op: "director.shot.export_preview", UserID: "owner", Caller: ExternalCaller(false),
		Params: json.RawMessage(`{"canvasId":"c1","nodeIds":["dir-1"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	proposal := proposed.Result.(map[string]any)
	if proposal["kind"] != DirectorPreviewKind || proposal["modelKey"] != DirectorClayPreviewModelKey || proposal["charged"] != false || proposal["local"] != true {
		t.Fatalf("白膜提议应为免费本地录制: %#v", proposal)
	}
	proposalID, _ := proposal["proposalId"].(string)
	if proposalID == "" {
		t.Fatal("提议缺少 proposalId")
	}
	source := proposal["source"].(map[string]any)
	if source["canvasId"] != "c1" || source["canvasRevision"] != float64(4) {
		t.Fatalf("提议应绑定画布版本: %#v", source)
	}
	if _, ok := source["modelConfigRevision"]; ok {
		t.Fatalf("白膜提议不应绑定付费模型配置版本: %#v", source)
	}

	params := directorConfirmParams(t, proposalID, "c1", []string{"dir-1"}, DirectorPreviewKind, DirectorClayPreviewModelKey, false, 4)
	first, err := registry.Execute(Request{
		Op: "canvas.generation.confirm", OpID: "confirm:" + proposalID, UserID: "owner", Caller: ExternalCaller(false), Params: params,
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted := first.Result.(map[string]any)
	if first.Replayed || accepted["status"] != "accepted" || accepted["charged"] != false || accepted["dispatched"] != true || accepted["taskCreated"] != false || accepted["kind"] != DirectorPreviewKind {
		t.Fatalf("确认应接受且不创建付费任务: replayed=%v %#v", first.Replayed, accepted)
	}
	pending := DefaultDirectorDispatchHub().Pending("owner", "c1")
	if len(pending) != 1 || pending[0].OperationID != "confirm:"+proposalID || pending[0].NodeIDs[0] != "dir-1" || pending[0].Charged || pending[0].ModelKey != DirectorClayPreviewModelKey {
		t.Fatalf("桌面应收到一次白膜投递: %#v", pending)
	}

	second, err := registry.Execute(Request{
		Op: "canvas.generation.confirm", OpID: "confirm:" + proposalID, UserID: "owner", Caller: ExternalCaller(false), Params: params,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed {
		t.Fatal("同一 confirm:提案ID 应回放原确认")
	}
	if len(DefaultDirectorDispatchHub().Pending("owner", "c1")) != 1 {
		t.Fatal("回放不应再排一次录制")
	}
	if !DefaultDirectorDispatchHub().Ack("owner", "c1", "confirm:"+proposalID) {
		t.Fatal("桌面应能确认已经开始录制")
	}
	replayed, err := registry.Execute(Request{
		Op: "canvas.generation.confirm", OpID: "confirm:" + proposalID, UserID: "owner", Caller: ExternalCaller(false), Params: params,
	})
	if err != nil || !replayed.Replayed {
		t.Fatalf("ack 后重试仍应回放原确认: %#v %v", replayed, err)
	}
	if len(DefaultDirectorDispatchHub().Pending("owner", "c1")) != 0 {
		t.Fatal("已经开始的录制不应因重试再投递")
	}
}

func TestDirectorPreviewConfirmRejectsWrongNode(t *testing.T) {
	ResetDirectorDispatchHub()
	t.Cleanup(ResetDirectorDispatchHub)
	video := imageNode("n1", "beefapi::wan3.0-video")
	video["type"] = "video"
	probe := &proposeProbe{doc: proposeCanvas(t, []map[string]any{video, directorNode("dir-1")})}
	_, err := opDirectorShotExportPreview(&Context{UserID: "owner", Domain: probe}, json.RawMessage(`{"canvasId":"c1","nodeIds":["n1"]}`))
	if err == nil || AsError(err).Reason != "generation_kind_mismatch" {
		t.Fatalf("视频节点不能提议白膜: %v", err)
	}
	_, err = opCanvasGenerationConfirm(&Context{UserID: "owner", Domain: probe}, directorConfirmParams(t, "gp-1", "c1", []string{"n1"}, DirectorPreviewKind, DirectorClayPreviewModelKey, false, 4))
	if err == nil || AsError(err).Reason != "generation_kind_mismatch" {
		t.Fatalf("对视频节点确认白膜应拒绝: %v", err)
	}
	_, err = opCanvasGenerationConfirm(&Context{UserID: "owner", Domain: probe}, directorConfirmParams(t, "gp-2", "c1", []string{"dir-1"}, "video", "beefapi::wan3.0-video", true, 4))
	if err == nil || AsError(err).Reason != "generation_kind_mismatch" {
		t.Fatalf("导演节点不能按视频确认: %v", err)
	}
	if len(DefaultDirectorDispatchHub().Pending("owner", "c1")) != 0 {
		t.Fatal("被拒绝的确认不能通知桌面")
	}
}

func TestPaidGenerationConfirmDoesNotDispatchClayOrCreateTask(t *testing.T) {
	ResetDirectorDispatchHub()
	t.Cleanup(ResetDirectorDispatchHub)
	probe := &proposeProbe{doc: proposeCanvas(t, []map[string]any{imageNode("n1", "beefapi::gpt-image-2")})}
	result, err := opCanvasGenerationConfirm(&Context{UserID: "owner", Domain: probe}, directorConfirmParams(t, "gp-video", "c1", []string{"n1"}, "image", "beefapi::gpt-image-2", true, 4))
	if err != nil {
		t.Fatal(err)
	}
	payload := result.(map[string]any)
	if payload["kind"] != "image" || payload["status"] != "accepted" || payload["dispatched"] != false || payload["taskCreated"] != false || payload["execution"] != "canvas_generation" {
		t.Fatalf("图片确认应保持原生成链路、不投递白膜: %#v", payload)
	}
	if len(DefaultDirectorDispatchHub().Pending("owner", "c1")) != 0 {
		t.Fatal("图片确认不能打开导演台")
	}
}

func TestDirectorPreviewConfirmRejectsBillingAndStaleRevision(t *testing.T) {
	probe := &proposeProbe{doc: proposeCanvas(t, []map[string]any{directorNode("dir-1")})}
	_, err := opCanvasGenerationConfirm(&Context{UserID: "owner", Domain: probe}, directorConfirmParams(t, "gp-1", "c1", []string{"dir-1"}, DirectorPreviewKind, DirectorClayPreviewModelKey, true, 4))
	if err == nil || AsError(err).Reason != "director_preview_not_billable" {
		t.Fatalf("白膜不能标成收费: %v", err)
	}
	_, err = opCanvasGenerationConfirm(&Context{UserID: "owner", Domain: probe}, directorConfirmParams(t, "gp-1", "c1", []string{"dir-1"}, DirectorPreviewKind, "beefapi::video", false, 4))
	if err == nil || AsError(err).Reason != "invalid_model" {
		t.Fatalf("白膜模型键必须是本地录制: %v", err)
	}
	_, err = opCanvasGenerationConfirm(&Context{UserID: "owner", Domain: probe}, directorConfirmParams(t, "gp-1", "c1", []string{"dir-1"}, DirectorPreviewKind, DirectorClayPreviewModelKey, false, 3))
	if err == nil || AsError(err).Reason != "canvas_revision_conflict" {
		t.Fatalf("过期画布版本应拒绝: %v", err)
	}
}

func TestDirectorPreviewConfirmRequiresStableOperationID(t *testing.T) {
	probe := &proposeProbe{doc: proposeCanvas(t, []map[string]any{directorNode("dir-1")})}
	db := directorPreviewDB(t)
	registry := NewRegistry(binderFunc(func(*gorm.DB) Domain { return probe }), NewStore(db))
	RegisterDefaultOps(registry)
	params := directorConfirmParams(t, "gp-1", "c1", []string{"dir-1"}, DirectorPreviewKind, DirectorClayPreviewModelKey, false, 4)
	_, err := registry.Execute(Request{
		Op: "canvas.generation.confirm", OpID: "other-key", UserID: "owner", Caller: ManualCaller(false), Params: params,
	})
	if err == nil || AsError(err).Reason != "effect_identity_mismatch" {
		t.Fatalf("确认幂等键必须是 confirm:提案ID: %v", err)
	}
}

type binderFunc func(*gorm.DB) Domain

func (f binderFunc) BindDomain(tx *gorm.DB) Domain { return f(tx) }

func directorPreviewDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "director-preview.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateLocalSchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func directorConfirmParams(t *testing.T, proposalID, canvasID string, nodeIDs []string, kind, modelKey string, charged bool, revision int64) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"proposalId": proposalID,
		"canvasId":   canvasID,
		"nodeIds":    nodeIDs,
		"kind":       kind,
		"modelKey":   modelKey,
		"charged":    charged,
		"source":     map[string]any{"canvasId": canvasID, "canvasRevision": revision},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
