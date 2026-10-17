package operations_test

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/operations"
)

func TestCanvasViewportFitLeavesNodesAndReplays(t *testing.T) {
	h := newHarness(t)
	encoded, err := json.Marshal(map[string]any{
		"id": "far-canvas", "title": "远处", "revision": 0,
		"nodes": []any{
			map[string]any{"id": "far", "type": "video", "title": "远处", "position": map[string]any{"x": -1500, "y": 5800}, "width": 200, "height": 100, "metadata": map[string]any{"prompt": "keep"}},
			map[string]any{"id": "near", "type": "text", "title": "近处", "position": map[string]any{"x": 10, "y": 20}, "width": 100, "height": 40, "metadata": map[string]any{"content": "stay"}},
		},
		"connections": []any{},
		"customKeep":  "stay",
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := h.service.UpsertUserCanvasProject(h.userID, encoded)
	if err != nil {
		t.Fatal(err)
	}
	before := readCanvas(t, h, "far-canvas")
	params := map[string]any{
		"canvasId": "far-canvas", "expectedRevision": created.Revision,
		"viewportSize": map[string]any{"width": 1440, "height": 900},
	}
	first, err := h.execute(t, operations.ExternalCaller(false), "canvas.viewport.fit", "fit-all-once", params)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed {
		t.Fatal("first call replayed")
	}
	payload := first.Result.(map[string]any)
	if payload["target"] != "all" || payload["fittedNodeCount"] != float64(2) {
		t.Fatalf("result=%v", payload)
	}
	viewport := payload["viewport"].(map[string]any)
	if viewport["k"].(float64) >= 1 || viewport["k"].(float64) < 0.05 {
		t.Fatalf("scale=%v", viewport["k"])
	}
	after := readCanvas(t, h, "far-canvas")
	if !jsonEqual(t, before["nodes"], after["nodes"]) || !jsonEqual(t, before["connections"], after["connections"]) || after["customKeep"] != "stay" {
		t.Fatalf("节点图被改写 before=%v after=%v", before["nodes"], after["nodes"])
	}
	if after["viewportSeq"] != payload["viewportSeq"] {
		t.Fatalf("seq stored=%v result=%v", after["viewportSeq"], payload["viewportSeq"])
	}
	checkpoint := payload["checkpoint"].(map[string]any)
	if checkpoint["revision"] != float64(created.Revision) || checkpoint["viewportSeq"] != float64(0) {
		t.Fatalf("checkpoint=%v", checkpoint)
	}
	rolled, err := h.execute(t, operations.ExternalCaller(false), "canvas.viewport.fit", "fit-all-rollback", map[string]any{
		"canvasId": "far-canvas", "expectedRevision": first.Revision,
		"viewport": checkpoint["viewport"],
	})
	if err != nil {
		t.Fatal(err)
	}
	restored := readCanvas(t, h, "far-canvas")
	if !jsonEqual(t, restored["nodes"], before["nodes"]) || !jsonEqual(t, restored["connections"], before["connections"]) || !jsonEqual(t, restored["viewport"], checkpoint["viewport"]) {
		t.Fatalf("用检查点回滚改动了节点或没有回到原相机: %v", restored["viewport"])
	}
	if rolled.Result.(map[string]any)["viewportSeq"] != float64(2) {
		t.Fatal("回滚没有前进视口序号")
	}
	replay, err := h.execute(t, operations.ExternalCaller(false), "canvas.viewport.fit", "fit-all-once", params)
	if err != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if readCanvas(t, h, "far-canvas")["viewportSeq"] != restored["viewportSeq"] {
		t.Fatal("重放再次推进了视口序号")
	}
	params["expectedRevision"] = created.Revision
	if _, err := h.execute(t, operations.ExternalCaller(false), "canvas.viewport.fit", "fit-stale", params); err == nil {
		t.Fatal("过期 revision 被接受")
	}
	if _, err := h.execute(t, operations.ExternalCaller(true), "canvas.viewport.fit", "fit-readonly", map[string]any{
		"canvasId": "far-canvas", "expectedRevision": revisionOf(t, h, "far-canvas"),
		"viewport": map[string]any{"x": 1, "y": 2, "k": 1},
	}); err == nil {
		t.Fatal("只读客户端被允许改视口")
	}

	current := revisionOf(t, h, "far-canvas")
	commitDoc := map[string]any{
		"title": "提交改名", "nodes": before["nodes"], "connections": before["connections"], "customKeep": "stay",
		"viewport": map[string]any{"x": 3, "y": 4, "k": 2}, "viewportSeq": 99,
	}
	if _, err := h.execute(t, operations.ExternalCaller(false), "canvas.document.commit", "commit-cannot-set-viewport", map[string]any{
		"canvasId": "far-canvas", "expectedRevision": current, "document": commitDoc,
	}); err != nil {
		t.Fatal(err)
	}
	committed := readCanvas(t, h, "far-canvas")
	if committed["title"] != "提交改名" {
		t.Fatalf("commit 没有改标题: %v", committed["title"])
	}
	if !jsonEqual(t, committed["viewport"], restored["viewport"]) || committed["viewportSeq"] != restored["viewportSeq"] {
		t.Fatalf("document.commit 改动了视口: viewport=%v seq=%v", committed["viewport"], committed["viewportSeq"])
	}
	if !jsonEqual(t, committed["nodes"], before["nodes"]) {
		t.Fatal("对照提交改写了节点")
	}
}

func readCanvas(t *testing.T, h *harness, id string) map[string]any {
	t.Helper()
	raw, err := h.service.UserCanvasProject(h.userID, id)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func revisionOf(t *testing.T, h *harness, id string) int64 {
	t.Helper()
	return int64(readCanvas(t, h, id)["revision"].(float64))
}

func jsonEqual(t *testing.T, left, right any) bool {
	t.Helper()
	a, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(right)
	if err != nil {
		t.Fatal(err)
	}
	return string(a) == string(b)
}
