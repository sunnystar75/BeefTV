package operations

import (
	"encoding/json"
	"strings"

	"infinite-canvas/backend/internal/canvas"
)

func registerCanvasViewportOps(r *Registry) {
	r.Register(Op{
		ReplaySafe: true, AssistantCanvas: true, ID: "canvas.viewport.fit",
		Summary: "只移动画布视角：对准全部可见节点、指定节点或包围盒，或直接设置 {x,y,k}。不修改节点图，也不能代替 canvas.document.commit",
		Scope:   ScopeCanvas,
		Params:  json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"canvasId":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1},"viewport":{"type":"object","additionalProperties":false,"properties":{"x":{"type":"number"},"y":{"type":"number"},"k":{"type":"number"}},"required":["x","y","k"]},"nodeIds":{"type":"array","maxItems":200,"items":{"type":"string"}},"bbox":{"type":"object","additionalProperties":false,"properties":{"left":{"type":"number"},"top":{"type":"number"},"right":{"type":"number"},"bottom":{"type":"number"}},"required":["left","top","right","bottom"]},"viewportSize":{"type":"object","additionalProperties":false,"properties":{"width":{"type":"number"},"height":{"type":"number"}},"required":["width","height"]},"padding":{"type":"number"},"maxScale":{"type":"number"}},"required":["canvasId","expectedRevision"]}`),
		Handler: opCanvasViewportFit,
	})
}

func opCanvasViewportFit(ctx *Context, params json.RawMessage) (any, error) {
	var args struct {
		CanvasID         string   `json:"canvasId"`
		ExpectedRevision *int64   `json:"expectedRevision"`
		NodeIDs          []string `json:"nodeIds"`
		Padding          *float64 `json:"padding"`
		MaxScale         *float64 `json:"maxScale"`
		Viewport         *struct {
			X *float64 `json:"x"`
			Y *float64 `json:"y"`
			K *float64 `json:"k"`
		} `json:"viewport"`
		BBox *struct {
			Left   *float64 `json:"left"`
			Top    *float64 `json:"top"`
			Right  *float64 `json:"right"`
			Bottom *float64 `json:"bottom"`
		} `json:"bbox"`
		ViewportSize *struct {
			Width  *float64 `json:"width"`
			Height *float64 `json:"height"`
		} `json:"viewportSize"`
	}
	if err := decodeParams(params, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.CanvasID) == "" || args.ExpectedRevision == nil || *args.ExpectedRevision < 1 {
		return nil, InvalidArg("invalid_params", "canvasId 和有效 expectedRevision 必填")
	}
	cmd := canvas.ViewportCommand{NodeIDs: args.NodeIDs, Padding: args.Padding, MaxScale: args.MaxScale}
	if args.Viewport != nil {
		if args.Viewport.X == nil || args.Viewport.Y == nil || args.Viewport.K == nil {
			return nil, InvalidArg("invalid_viewport", "viewport.x、viewport.y、viewport.k 必填")
		}
		cmd.Viewport = &canvas.Viewport{X: *args.Viewport.X, Y: *args.Viewport.Y, K: *args.Viewport.K}
	}
	if args.BBox != nil {
		if args.BBox.Left == nil || args.BBox.Top == nil || args.BBox.Right == nil || args.BBox.Bottom == nil {
			return nil, InvalidArg("invalid_bbox", "bbox.left、top、right、bottom 必填")
		}
		cmd.BBox = &canvas.CanvasBounds{Left: *args.BBox.Left, Top: *args.BBox.Top, Right: *args.BBox.Right, Bottom: *args.BBox.Bottom}
	}
	if args.ViewportSize != nil {
		if args.ViewportSize.Width == nil || args.ViewportSize.Height == nil {
			return nil, InvalidArg("invalid_viewport_size", "viewportSize.width 和 viewportSize.height 必填")
		}
		cmd.Size = &canvas.ViewportSize{Width: *args.ViewportSize.Width, Height: *args.ViewportSize.Height}
	}
	fitted, err := ctx.Domain.SetUserCanvasViewport(ctx.UserID, args.CanvasID, *args.ExpectedRevision, cmd)
	if err != nil {
		return nil, mapDomainError(err)
	}
	result := map[string]any{
		"canvasId":    args.CanvasID,
		"revision":    fitted.Summary.Revision,
		"viewport":    map[string]any{"x": fitted.Viewport.X, "y": fitted.Viewport.Y, "k": fitted.Viewport.K},
		"viewportSeq": fitted.Seq,
		"target":      fitted.Target,
	}
	if fitted.Target != "exact" {
		result["fittedNodeCount"] = fitted.FittedNodeCount
	}
	if fitted.Bounds != nil {
		result["bounds"] = map[string]any{
			"left": fitted.Bounds.Left, "top": fitted.Bounds.Top,
			"right": fitted.Bounds.Right, "bottom": fitted.Bounds.Bottom,
		}
	}
	return result, nil
}
