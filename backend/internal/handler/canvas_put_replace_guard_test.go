package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExternalCanvasPutRejectsPartialNodeReplace(t *testing.T) {
	env := newAssistantTestEnv(t, nil)
	api := env.router.Group("/api")
	api.Use(RuntimeDependenciesMiddleware(RuntimeDependencies{RequestCoordinator: &stubRequestCoordinator{allowed: true}}))
	RegisterDesktopUserDataRoutes(api, env.service)
	owner, err := env.service.LocalWorkspaceOwner()
	if err != nil {
		t.Fatal(err)
	}
	created, err := env.service.UpsertUserCanvasProject(owner.ID, []byte(`{
		"id":"wipe-canvas","revision":0,"title":"多节点",
		"nodes":[{"id":"n1","type":"text","title":"一"},{"id":"n2","type":"text","title":"二"}],
		"connections":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	partial := map[string]any{
		"project": map[string]any{
			"id": "wipe-canvas", "revision": created.Revision, "title": "残缺",
			"nodes":       []any{map[string]any{"id": "n2", "type": "text", "title": "只剩一个"}},
			"connections": []any{},
		},
	}
	code, body := putCanvas(t, env, partial, true)
	if code != http.StatusPreconditionFailed || !bytes.Contains(body, []byte(`"reason":"full_document_replace_required"`)) || !bytes.Contains(body, []byte("CAS 覆盖")) {
		t.Fatalf("外部残缺 PUT 必须拒绝: %d %s", code, body)
	}
	if count := canvasNodeCount(t, env, owner.ID, "wipe-canvas"); count != 2 {
		t.Fatalf("拒绝后节点数 = %d", count)
	}
	kept := map[string]any{
		"project": map[string]any{
			"id": "wipe-canvas", "revision": created.Revision, "title": "改标题",
			"nodes": []any{
				map[string]any{"id": "n1", "type": "text", "title": "一"},
				map[string]any{"id": "n2", "type": "text", "title": "二"},
			},
			"connections": []any{},
		},
	}
	code, body = putCanvas(t, env, kept, true)
	if code != http.StatusOK {
		t.Fatalf("保留全部节点的外部 PUT 应成功: %d %s", code, body)
	}
	current := canvasRevision(t, env, owner.ID, "wipe-canvas")
	flagged := map[string]any{
		"allowFullDocumentReplace": true,
		"project": map[string]any{
			"id": "wipe-canvas", "revision": current, "title": "显式替换",
			"nodes":       []any{map[string]any{"id": "n2", "type": "text", "title": "只剩一个"}},
			"connections": []any{},
		},
	}
	code, body = putCanvas(t, env, flagged, true)
	if code != http.StatusOK {
		t.Fatalf("显式标志应允许整布替换: %d %s", code, body)
	}
	if count := canvasNodeCount(t, env, owner.ID, "wipe-canvas"); count != 1 {
		t.Fatalf("显式替换后节点数 = %d", count)
	}

	desktop, err := env.service.UpsertUserCanvasProject(owner.ID, []byte(`{
		"id":"desktop-canvas","revision":0,"title":"桌面",
		"nodes":[{"id":"a","type":"text","title":"甲"},{"id":"b","type":"text","title":"乙"}],
		"connections":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	code, body = putCanvas(t, env, map[string]any{
		"project": map[string]any{
			"id": "desktop-canvas", "revision": desktop.Revision, "title": "桌面删除",
			"nodes": []any{map[string]any{"id": "a", "type": "text", "title": "甲"}}, "connections": []any{},
		},
	}, false)
	if code != http.StatusOK {
		t.Fatalf("非外部 PUT 应保留编辑器删除: %d %s", code, body)
	}
}

func putCanvas(t *testing.T, env *assistantTestEnv, body any, external bool) (int, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/canvas-projects/ignored", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/api/canvas-projects/" + bodyProjectID(t, body)
	if external {
		req = WithExternalBusinessPrincipal(req, false)
	}
	recorder := httptest.NewRecorder()
	env.router.ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.Bytes()
}

func bodyProjectID(t *testing.T, body any) string {
	t.Helper()
	encoded, _ := json.Marshal(body)
	var payload struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil || payload.Project.ID == "" {
		t.Fatalf("project id: %v %s", err, encoded)
	}
	return payload.Project.ID
}

func canvasNodeCount(t *testing.T, env *assistantTestEnv, userID, canvasID string) int {
	t.Helper()
	raw, err := env.service.UserCanvasProject(userID, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return len(doc.Nodes)
}

func canvasRevision(t *testing.T, env *assistantTestEnv, userID, canvasID string) int64 {
	t.Helper()
	raw, err := env.service.UserCanvasProject(userID, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Revision
}
