package canvas

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"infinite-canvas/backend/internal/kernel"
)

func TestSetUserCanvasViewportFitsVisibleNodesWithoutRewritingThem(t *testing.T) {
	svc := newCanvasHistoryTestService(t)
	created, err := svc.UpsertUserCanvasProject("owner", json.RawMessage(`{
		"id":"canvas","revision":0,"title":"远节点",
		"nodes":[
			{"id":"frame","type":"frame","title":"框","position":{"x":0,"y":0},"width":100,"height":80,"metadata":{"frame":{"collapsed":true}}},
			{"id":"hidden-child","type":"text","title":"折叠里","parentId":"frame","position":{"x":0,"y":15705},"width":400,"height":200,"metadata":{}},
			{"id":"far","type":"video","title":"远处","position":{"x":-1500,"y":5800},"width":6043,"height":200,"metadata":{"prompt":"keep"}}
		],
		"connections":[{"id":"e1","fromNodeId":"frame","toNodeId":"far"}],
		"customKeep":"stay"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.UserCanvasProject("owner", "canvas")
	if err != nil {
		t.Fatal(err)
	}
	beforeNodes := rawField(t, before, "nodes")
	beforeEdges := rawField(t, before, "connections")
	padding := 0.0
	maxScale := 1.0
	fitted, err := svc.SetUserCanvasViewport("owner", "canvas", created.Revision, ViewportCommand{
		Size:     &ViewportSize{Width: 200, Height: 200},
		Padding:  &padding,
		MaxScale: &maxScale,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fitted.Summary.Revision != created.Revision+1 || fitted.Seq != 1 || fitted.Target != "all" || fitted.FittedNodeCount != 2 {
		t.Fatalf("result=%+v", fitted)
	}
	// 可见节点是框 (0,0,100,80) 与远处节点 (-1500,5800,4543,6000)。折叠子节点不参与。
	if fitted.Bounds == nil || fitted.Bounds.Left != -1500 || fitted.Bounds.Top != 0 || fitted.Bounds.Right != 4543 || fitted.Bounds.Bottom != 6000 {
		t.Fatalf("bounds=%+v", fitted.Bounds)
	}
	after, err := svc.UserCanvasProject("owner", "canvas")
	if err != nil {
		t.Fatal(err)
	}
	if rawField(t, after, "nodes") != beforeNodes || rawField(t, after, "connections") != beforeEdges {
		t.Fatalf("视口写入改写了节点图\nbefore nodes=%s\nafter nodes=%s", beforeNodes, rawField(t, after, "nodes"))
	}
	var doc map[string]any
	if err := json.Unmarshal(after, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["customKeep"] != "stay" || doc["title"] != "远节点" {
		t.Fatalf("其它字段被改写: %s", after)
	}
	viewport := doc["viewport"].(map[string]any)
	if viewport["k"] != fitted.Viewport.K || viewport["x"] != fitted.Viewport.X || viewport["y"] != fitted.Viewport.Y {
		t.Fatalf("stored viewport=%v result=%+v", viewport, fitted.Viewport)
	}
	if doc["viewportSeq"] != float64(1) {
		t.Fatalf("viewportSeq=%v", doc["viewportSeq"])
	}
	history, err := svc.CanvasHistory("owner", "canvas")
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Snapshots) != 0 {
		t.Fatalf("只改视口不应产生内容快照: %+v", history.Snapshots)
	}

	// 随后的整份文档保存必须留下这次视口，并忽略提交体里的 viewport。
	saved, err := svc.UpsertUserCanvasProject("owner", json.RawMessage(`{
		"id":"canvas","revision":`+itoa(fitted.Summary.Revision)+`,"title":"改标题",
		"nodes":[{"id":"only"}],
		"viewport":{"x":9,"y":9,"k":2},
		"viewportSeq":99
	}`))
	if err != nil {
		t.Fatal(err)
	}
	kept, err := svc.UserCanvasProject("owner", "canvas")
	if err != nil {
		t.Fatal(err)
	}
	var keptDoc map[string]any
	if err := json.Unmarshal(kept, &keptDoc); err != nil {
		t.Fatal(err)
	}
	if keptDoc["viewportSeq"] != float64(1) || keptDoc["viewport"].(map[string]any)["x"] != fitted.Viewport.X {
		t.Fatalf("文档保存冲掉了视口: rev=%d doc=%s", saved.Revision, kept)
	}
	if len(keptDoc["nodes"].([]any)) != 1 {
		t.Fatal("对照用的文档保存没有按提交体替换节点")
	}
}

func TestSetUserCanvasViewportExactNodesAndBBox(t *testing.T) {
	svc := newCanvasHistoryTestService(t)
	created, err := svc.UpsertUserCanvasProject("owner", json.RawMessage(`{
		"id":"canvas","revision":0,"title":"选择",
		"nodes":[
			{"id":"a","type":"text","title":"A","position":{"x":0,"y":0},"width":100,"height":100},
			{"id":"b","type":"text","title":"B","position":{"x":1000,"y":1000},"width":100,"height":100}
		],
		"connections":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	padding := 0.0
	selected, err := svc.SetUserCanvasViewport("owner", "canvas", created.Revision, ViewportCommand{
		NodeIDs: []string{"b", "b"},
		Size:    &ViewportSize{Width: 200, Height: 200},
		Padding: &padding,
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Target != "nodes" || selected.FittedNodeCount != 1 || selected.Bounds.Left != 1000 {
		t.Fatalf("selected=%+v bounds=%+v", selected, selected.Bounds)
	}
	exact, err := svc.SetUserCanvasViewport("owner", "canvas", selected.Summary.Revision, ViewportCommand{
		Viewport: &Viewport{X: 12, Y: -4, K: 0.5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if exact.Target != "exact" || exact.Seq != 2 || exact.Bounds != nil || exact.Viewport.K != 0.5 {
		t.Fatalf("exact=%+v", exact)
	}
	box, err := svc.SetUserCanvasViewport("owner", "canvas", exact.Summary.Revision, ViewportCommand{
		BBox:    &CanvasBounds{Left: 0, Top: 0, Right: 100, Bottom: 100},
		Size:    &ViewportSize{Width: 200, Height: 200},
		Padding: &padding,
	})
	if err != nil {
		t.Fatal(err)
	}
	if box.Target != "bbox" || box.Viewport.X != 50 || box.Viewport.Y != 50 || box.Viewport.K != 1 || box.Seq != 3 {
		t.Fatalf("bbox viewport=%+v", box.Viewport)
	}
	if _, err := svc.SetUserCanvasViewport("owner", "canvas", box.Summary.Revision, ViewportCommand{
		Viewport: &Viewport{X: 1, Y: 1, K: 0.01},
	}); err == nil {
		t.Fatal("过小缩放被接受")
	}
	var appErr *kernel.AppError
	if _, err := svc.SetUserCanvasViewport("owner", "canvas", 1, ViewportCommand{Viewport: &Viewport{X: 1, Y: 1, K: 1}}); !errors.As(err, &appErr) || appErr.Status != http.StatusConflict {
		t.Fatalf("过期 revision=%v", err)
	}
}

func rawField(t *testing.T, raw json.RawMessage, key string) string {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return string(payload[key])
}

func itoa(value int64) string {
	return jsonNumber(value)
}

func jsonNumber(value int64) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
