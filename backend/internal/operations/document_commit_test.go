package operations_test

import (
	"encoding/json"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentops"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/operations"
)

func TestDocumentCommitManualAndAgentInterleaving(t *testing.T) {
	h := newHarness(t)
	overlay := map[string]any{
		"id": h.canvasID, "title": "手工整页",
		"nodes": []any{
			map[string]any{"id": "n1", "type": "image", "title": "手工镜头", "position": map[string]any{"x": 1, "y": 1},
				"width": 320, "height": 220, "metadata": map[string]any{"prompt": "手工"}},
			map[string]any{"id": "n2", "type": "image", "title": "镜头2", "position": map[string]any{"x": 2, "y": 2},
				"width": 320, "height": 220, "metadata": map[string]any{"prompt": "原始2"}},
		},
		"connections": []any{},
	}
	first, err := h.execute(t, operations.ManualCaller(false), "canvas.document.commit", "manual-doc-1", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": h.revisionNow(t), "document": overlay,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.Caller != operations.CallerManual || first.Revision == 0 {
		t.Fatalf("手工提交信封异常: %#v", first)
	}
	agentRev := h.revisionNow(t)
	agent, err := h.execute(t, h.assistant(), "canvas.node.update", "agent-patch-1", map[string]any{
		"canvasId": h.canvasID, "nodeId": "n1", "expectedRevision": agentRev,
		"patch": map[string]any{"title": "助手改名"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Revision == first.Revision {
		t.Fatal("助手写入应推进 revision")
	}
	_, err = h.execute(t, operations.ManualCaller(false), "canvas.document.commit", "manual-stale", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": first.Revision, "document": overlay,
	})
	if got := opErr(t, err); got.Code != operations.CodeConflict {
		t.Fatalf("陈旧手工提交必须冲突: %v", err)
	}
	raw, err := h.service.UserCanvasProject(h.userID, h.canvasID)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	nodes := stored["nodes"].([]any)
	title, _ := nodes[0].(map[string]any)["title"].(string)
	if title != "助手改名" {
		t.Fatalf("陈旧手工提交不得覆盖助手改动: %s", raw)
	}
}

func TestDocumentCommitReplayAndPayloadConflict(t *testing.T) {
	h := newHarness(t)
	params := map[string]any{
		"canvasId": h.canvasID, "expectedRevision": h.revisionNow(t),
		"document": map[string]any{"title": "幂等提交", "nodes": []any{}, "connections": []any{}},
	}
	first, err := h.execute(t, operations.ManualCaller(false), "canvas.document.commit", "doc-replay", params)
	if err != nil {
		t.Fatal(err)
	}
	before := h.revisionNow(t)
	replay, err := h.execute(t, operations.ExternalCaller(false), "canvas.document.commit", "doc-replay", params)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.Revision != first.Revision {
		t.Fatalf("丢失应答重放必须回读原结果: %#v", replay)
	}
	if h.revisionNow(t) != before {
		t.Fatal("重放推进了画布版本")
	}
	var receipts int64
	if err := h.service.Database().Model(&model.AgentOpRecord{}).Where("user_id = ? AND op_id = ?", h.userID, "doc-replay").Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("同一操作应只有一条回执，得到 %d", receipts)
	}
	_, err = h.execute(t, operations.ManualCaller(false), "canvas.document.commit", "doc-replay", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": h.revision,
		"document": map[string]any{"title": "不同内容", "nodes": []any{}, "connections": []any{}},
	})
	if got := opErr(t, err); got.Reason != "operation_id_reused_with_different_payload" {
		t.Fatalf("不同 payload 应为冲突，得到 %v", err)
	}
}

func TestAssistantCannotCommitDocument(t *testing.T) {
	h := newHarness(t)
	for _, descriptor := range h.registry.List(h.assistant()) {
		if descriptor.ID == "canvas.document.commit" {
			t.Fatal("助手能力发现不得包含整页文档提交")
		}
	}
	_, err := h.execute(t, h.assistant(), "canvas.document.commit", "assistant-doc", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": h.revisionNow(t),
		"document": map[string]any{"title": "越权", "nodes": []any{}, "connections": []any{}},
	})
	if got := opErr(t, err); got.Code != operations.CodePermissionDenied {
		t.Fatalf("助手执行整页提交应被拒: %v", err)
	}
	empty := operations.AssistantCaller(&agentops.AssistantScope{}, false)
	_, err = h.execute(t, empty, "canvas.document.commit", "empty-scope-doc", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": h.revisionNow(t),
		"document": map[string]any{"title": "空范围", "nodes": []any{}, "connections": []any{}},
	})
	if got := opErr(t, err); got.Code != operations.CodePermissionDenied {
		t.Fatalf("空助手范围执行整页提交应被拒: %v", err)
	}
}

func TestDocumentCommitRequiresExpectedRevision(t *testing.T) {
	h := newHarness(t)
	_, err := h.execute(t, operations.ManualCaller(false), "canvas.document.commit", "missing-rev", map[string]any{
		"canvasId": h.canvasID, "document": map[string]any{"title": "缺版本"},
	})
	if got := opErr(t, err); got.Code != operations.CodeInvalidArgument {
		t.Fatalf("缺少 expectedRevision 应被拒: %v", err)
	}
}

func TestExternalPartialDocumentCommitRejectedUntilExplicitReplace(t *testing.T) {
	h := newHarness(t)
	before := h.revisionNow(t)
	partial := map[string]any{
		"nodes": []any{map[string]any{"id": "n2", "type": "director", "title": "只提交一个节点"}},
	}
	_, err := h.execute(t, operations.ExternalCaller(false), "canvas.document.commit", "doc-bind-partial", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": before, "document": partial,
	})
	got := opErr(t, err)
	if got.Code != operations.CodePreconditionFailed || got.Reason != "full_document_replace_required" {
		t.Fatalf("单节点提交必须拒绝: %v", err)
	}
	if got.Message == "" || !containsAll(got.Message, "CAS 覆盖", "allowFullDocumentReplace") {
		t.Fatalf("拒绝文案必须说明整份覆盖: %s", got.Message)
	}
	if got.Details["currentNodeCount"] != float64(2) && got.Details["currentNodeCount"] != 2 {
		t.Fatalf("details = %#v", got.Details)
	}
	if h.revisionNow(t) != before || nodeCount(t, h) != 2 {
		t.Fatal("被拒绝的提交不得改画布")
	}
	_, err = h.execute(t, operations.ExternalCaller(false), "canvas.document.commit", "doc-bind-flag-inside", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": before,
		"document": map[string]any{"allowFullDocumentReplace": true, "nodes": partial["nodes"]},
	})
	if opErr(t, err).Reason != "full_document_replace_required" {
		t.Fatalf("写进 document 正文的标志不得放行: %v", err)
	}

	renamed, err := h.execute(t, operations.ExternalCaller(false), "canvas.node.update", "rename-after-reject", map[string]any{
		"canvasId": h.canvasID, "nodeId": "n1", "expectedRevision": h.revisionNow(t),
		"patch": map[string]any{"title": "细粒度仍可用"},
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := h.execute(t, operations.ExternalCaller(false), "canvas.nodes.create", "create-after-reject", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": renamed.Revision,
		"nodes": []any{map[string]any{"title": "新节点", "type": "text"}},
	})
	if err != nil || nodeCount(t, h) != 3 {
		t.Fatalf("细粒度创建应成功: %v count=%d", err, nodeCount(t, h))
	}

	full := storedDocument(t, h)
	full["title"] = "整表但保留节点"
	if _, err := h.execute(t, operations.ExternalCaller(false), "canvas.document.commit", "full-keep", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": created.Revision, "document": full,
	}); err != nil {
		t.Fatalf("保留全部节点 ID 的外部提交应成功: %v", err)
	}
	if nodeCount(t, h) != 3 {
		t.Fatalf("保留提交后节点数 = %d", nodeCount(t, h))
	}

	replaced, err := h.execute(t, operations.ExternalCaller(false), "canvas.document.commit", "explicit-wipe", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": h.revisionNow(t), "allowFullDocumentReplace": true,
		"document": partial,
	})
	if err != nil {
		t.Fatal(err)
	}
	if nodeCount(t, h) != 1 || replaced.Revision == created.Revision {
		t.Fatalf("显式整布替换应留下 1 个节点: rev=%d count=%d", replaced.Revision, nodeCount(t, h))
	}
	if _, err := h.execute(t, operations.ExternalCaller(false), "canvas.document.commit", "stale-flag", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": before, "allowFullDocumentReplace": true,
		"document": partial,
	}); opErr(t, err).Code != operations.CodeConflict {
		t.Fatalf("标志不得绕过 CAS: %v", err)
	}
}

func TestManualDocumentCommitCanShrinkWithoutFlag(t *testing.T) {
	h := newHarness(t)
	_, err := h.execute(t, operations.ManualCaller(false), "canvas.document.commit", "manual-shrink", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": h.revisionNow(t),
		"document": map[string]any{"nodes": []any{map[string]any{"id": "n1", "type": "image", "title": "编辑器删除后的整页"}}},
	})
	if err != nil || nodeCount(t, h) != 1 {
		t.Fatalf("桌面手工保存应能删节点: %v count=%d", err, nodeCount(t, h))
	}
}

func TestOwnerTokenStyleCommitRequiresReplaceFlag(t *testing.T) {
	h := newHarness(t)
	ownerCLI := operations.Caller{Kind: operations.CallerManual, GuardFullDocumentReplace: true}
	_, err := h.execute(t, ownerCLI, "canvas.document.commit", "owner-partial", map[string]any{
		"canvasId": h.canvasID, "expectedRevision": h.revisionNow(t),
		"document": map[string]any{"nodes": []any{map[string]any{"id": "n1", "type": "text", "title": "owner cli"}}},
	})
	if opErr(t, err).Reason != "full_document_replace_required" || nodeCount(t, h) != 2 {
		t.Fatalf("owner CLI 残缺提交必须拒绝: %v", err)
	}
}

func storedDocument(t *testing.T, h *harness) map[string]any {
	t.Helper()
	raw, err := h.service.UserCanvasProject(h.userID, h.canvasID)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	return stored
}

func nodeCount(t *testing.T, h *harness) int {
	t.Helper()
	nodes, _ := storedDocument(t, h)["nodes"].([]any)
	return len(nodes)
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
