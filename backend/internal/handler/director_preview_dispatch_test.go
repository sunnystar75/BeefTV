package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentops"
	"infinite-canvas/backend/internal/operations"
)

func TestDirectorPreviewConfirmFromExternalClientReachesDesktop(t *testing.T) {
	operations.ResetDirectorDispatchHub()
	t.Cleanup(operations.ResetDirectorDispatchHub)
	env := newAssistantTestEnv(t, nil)
	RegisterDirectorPreviewDispatchRoutes(env.router.Group("/api"), env.service)
	owner, err := env.service.LocalWorkspaceOwner()
	if err != nil {
		t.Fatal(err)
	}
	canvasID := "director-canvas"
	encoded, err := json.Marshal(map[string]any{
		"id": canvasID, "title": "导演画布", "revision": 0,
		"nodes": []any{map[string]any{
			"id": "dir-1", "type": "director", "title": "导演台 1",
			"position": map[string]any{"x": 0, "y": 0},
			"metadata": map[string]any{"workflowKind": "shot"},
		}},
		"connections": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.service.UpsertUserCanvasProject(owner.ID, encoded); err != nil {
		t.Fatal(err)
	}
	raw, err := env.service.UserCanvasProject(owner.ID, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	revision := int64(stored["revision"].(float64))
	client, token, err := env.clients.RegisterKind("codex", "xiao-ying", agentops.ClientReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"X-Beeftv-Client": client.ID, "Authorization": "Bearer " + token}

	status, body := env.opsWithHeaders(t, http.MethodPost, "/ops/director.shot.export_preview", `{"params":{"canvasId":"`+canvasID+`","nodeIds":["dir-1"]}}`, headers)
	if status != http.StatusOK {
		t.Fatalf("外部客户端提议白膜失败: %d %s", status, body)
	}
	proposal := decodeData(t, body)
	result := proposal["result"].(map[string]any)
	if result["kind"] != "director_preview" || result["modelKey"] != "local::director-clay-preview" || result["charged"] != false {
		t.Fatalf("提议形状不对: %#v", result)
	}
	proposalID := result["proposalId"].(string)
	confirmBody, err := json.Marshal(map[string]any{
		"opId": "confirm:" + proposalID,
		"params": map[string]any{
			"proposalId": proposalID,
			"canvasId":   canvasID,
			"nodeIds":    []string{"dir-1"},
			"kind":       "director_preview",
			"modelKey":   "local::director-clay-preview",
			"charged":    false,
			"source":     map[string]any{"canvasId": canvasID, "canvasRevision": revision},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, body = env.opsWithHeaders(t, http.MethodPost, "/ops/canvas.generation.confirm", string(confirmBody), headers)
	if status != http.StatusOK {
		t.Fatalf("外部客户端确认白膜失败: %d %s", status, body)
	}
	confirmed := decodeData(t, body)["result"].(map[string]any)
	if confirmed["status"] != "accepted" || confirmed["charged"] != false || confirmed["taskCreated"] != false || confirmed["dispatched"] != true {
		t.Fatalf("确认应投递给桌面且不计费: %#v", confirmed)
	}

	status, body = env.opsWithHeaders(t, http.MethodGet, "/desktop/director-preview-dispatches?canvasId="+canvasID, "", nil)
	if status != http.StatusOK {
		t.Fatalf("桌面领取白膜确认失败: %d %s", status, body)
	}
	dispatches := decodeData(t, body)["dispatches"].([]any)
	if len(dispatches) != 1 {
		t.Fatalf("桌面应看到 1 条待录制: %#v", dispatches)
	}
	ack, err := json.Marshal(map[string]any{"canvasId": canvasID, "operationId": "confirm:" + proposalID})
	if err != nil {
		t.Fatal(err)
	}
	status, body = env.opsWithHeaders(t, http.MethodPost, "/desktop/director-preview-dispatches/ack", string(ack), nil)
	if status != http.StatusOK || decodeData(t, body)["acked"] != true {
		t.Fatalf("桌面 ack 失败: %d %s", status, body)
	}
}

func (e *assistantTestEnv) opsWithHeaders(t *testing.T, method, path, body string, headers map[string]string) (int, string) {
	t.Helper()
	request := httptest.NewRequest(method, "http://127.0.0.1:18090/api"+path, strings.NewReader(body))
	request.Host = "127.0.0.1:18090"
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	e.router.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String()
}

func decodeData(t *testing.T, body string) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("响应不是信封: %s", body)
	}
	return envelope.Data
}
