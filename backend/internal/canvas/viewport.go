package canvas

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/kernel"

	"gorm.io/gorm"
)

const (
	minViewportScale   = 0.05
	maxViewportScale   = 8
	defaultNodeWidth   = 320.0
	defaultNodeHeight  = 220.0
	defaultFitPadding  = 88.0
	defaultFitMaxScale = 1.0
	maxViewportCoord   = 1e7
	maxFitNodeIDs      = 200
)

// Viewport 是画布相机。x/y 是屏幕平移，k 是缩放，与界面 ViewportTransform 一致。
type Viewport struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	K float64 `json:"k"`
}

// ViewportSize 是适应视口时使用的窗口尺寸，单位为 CSS 像素。
type ViewportSize struct {
	Width  float64
	Height float64
}

// CanvasBounds 是世界坐标包围盒，右/下边界为开区间外沿（节点 position + size）。
type CanvasBounds struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

// ViewportCommand 描述一次只改相机的写入。Viewport、NodeIDs、BBox 至多出现一个；
// 都缺省时对准当前可见节点。
type ViewportCommand struct {
	Viewport *Viewport
	NodeIDs  []string
	BBox     *CanvasBounds
	Size     *ViewportSize
	Padding  *float64
	MaxScale *float64
}

// ViewportResult 是视口写入的回执。它不包含节点图。
type ViewportResult struct {
	Summary         UserDataSummary
	Viewport        Viewport
	Seq             int64
	Target          string
	FittedNodeCount int
	Bounds          *CanvasBounds
}

type resolvedViewport struct {
	Viewport  Viewport
	Target    string
	Count     int
	Bounds    CanvasBounds
	HasBounds bool
}

// SetUserCanvasViewport 只替换画布文档里的 viewport 与 viewportSeq。
// 节点、连线和其余顶层字段保持读到的原始 JSON，不经过 document.commit。
func (s *Service) SetUserCanvasViewport(userID, canvasID string, expectedRevision int64, cmd ViewportCommand) (ViewportResult, error) {
	if err := requireExpectedRevision(expectedRevision); err != nil {
		return ViewportResult{}, err
	}
	canvasID = strings.TrimSpace(canvasID)
	if canvasID == "" {
		return ViewportResult{}, kernel.BadAuthRequest("画布 ID 必填")
	}
	existing, err := s.repo.CanvasProjectForUser(userID, canvasID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ViewportResult{}, kernel.NewAppError(http.StatusNotFound, "画布不存在或无权访问")
	}
	if err != nil {
		return ViewportResult{}, err
	}
	payload := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(existing.PayloadJSON), &payload); err != nil {
		return ViewportResult{}, kernel.BadAuthRequest("画布数据格式错误")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(existing.PayloadJSON), &doc); err != nil {
		return ViewportResult{}, kernel.BadAuthRequest("画布数据格式错误")
	}
	fitted, err := resolveViewportCommand(doc, cmd)
	if err != nil {
		return ViewportResult{}, err
	}
	seq := storedViewportSeq(payload) + 1
	if seq <= 0 {
		return ViewportResult{}, kernel.BadAuthRequest("视口序号已用尽")
	}
	viewportRaw, err := json.Marshal(fitted.Viewport)
	if err != nil {
		return ViewportResult{}, err
	}
	seqRaw, err := json.Marshal(seq)
	if err != nil {
		return ViewportResult{}, err
	}
	idRaw, err := json.Marshal(canvasID)
	if err != nil {
		return ViewportResult{}, err
	}
	revRaw, err := json.Marshal(expectedRevision)
	if err != nil {
		return ViewportResult{}, err
	}
	payload["viewport"] = viewportRaw
	payload["viewportSeq"] = seqRaw
	payload["id"] = idRaw
	payload["revision"] = revRaw
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ViewportResult{}, err
	}
	summary, err := s.upsertUserCanvasProjectReplacingViewport(userID, encoded)
	if err != nil {
		return ViewportResult{}, err
	}
	result := ViewportResult{
		Summary: summary, Viewport: fitted.Viewport, Seq: seq,
		Target: fitted.Target, FittedNodeCount: fitted.Count,
	}
	if fitted.HasBounds {
		bounds := fitted.Bounds
		result.Bounds = &bounds
	}
	return result, nil
}

func storedViewportSeq(payload map[string]json.RawMessage) int64 {
	raw := payload["viewportSeq"]
	if len(raw) == 0 {
		return 0
	}
	var seq int64
	if json.Unmarshal(raw, &seq) != nil || seq < 0 {
		return 0
	}
	return seq
}

func resolveViewportCommand(doc map[string]any, cmd ViewportCommand) (resolvedViewport, error) {
	modes := 0
	if cmd.Viewport != nil {
		modes++
	}
	if cmd.NodeIDs != nil {
		modes++
	}
	if cmd.BBox != nil {
		modes++
	}
	if modes > 1 {
		return resolvedViewport{}, kernel.NewAppError(http.StatusBadRequest, "viewport、nodeIds、bbox 只能选一种")
	}
	if cmd.Viewport != nil {
		if err := validateViewport(*cmd.Viewport); err != nil {
			return resolvedViewport{}, err
		}
		return resolvedViewport{Viewport: *cmd.Viewport, Target: "exact"}, nil
	}
	if cmd.Size == nil || !validViewportSize(*cmd.Size) {
		return resolvedViewport{}, kernel.NewAppError(http.StatusBadRequest, "适应视口需要 viewportSize.width 和 viewportSize.height")
	}
	padding := defaultFitPadding
	if cmd.Padding != nil {
		padding = *cmd.Padding
	}
	if !validPadding(padding) {
		return resolvedViewport{}, kernel.NewAppError(http.StatusBadRequest, "padding 必须是非负有限数字")
	}
	maxScale := defaultFitMaxScale
	if cmd.MaxScale != nil {
		maxScale = *cmd.MaxScale
	}
	if math.IsNaN(maxScale) || math.IsInf(maxScale, 0) || maxScale < minViewportScale || maxScale > maxViewportScale {
		return resolvedViewport{}, kernel.NewAppError(http.StatusBadRequest, "maxScale 必须在 0.05 到 8 之间")
	}
	var (
		bounds CanvasBounds
		count  int
		target string
		err    error
	)
	switch {
	case cmd.BBox != nil:
		if err = validateBounds(*cmd.BBox); err != nil {
			return resolvedViewport{}, err
		}
		bounds = *cmd.BBox
		target = "bbox"
	case cmd.NodeIDs != nil:
		bounds, count, err = boundsForNodeIDs(doc, cmd.NodeIDs)
		target = "nodes"
	default:
		bounds, count, err = boundsForVisibleNodes(doc)
		target = "all"
	}
	if err != nil {
		return resolvedViewport{}, err
	}
	viewport, err := viewportForBounds(bounds, *cmd.Size, padding, minViewportScale, maxScale)
	if err != nil {
		return resolvedViewport{}, err
	}
	return resolvedViewport{Viewport: viewport, Target: target, Count: count, Bounds: bounds, HasBounds: true}, nil
}

func viewportForBounds(bounds CanvasBounds, size ViewportSize, padding, minScale, maxScale float64) (Viewport, error) {
	boundsWidth := math.Max(1, bounds.Right-bounds.Left)
	boundsHeight := math.Max(1, bounds.Bottom-bounds.Top)
	availableWidth := math.Max(1, size.Width-padding*2)
	availableHeight := math.Max(1, size.Height-padding*2)
	k := math.Min(maxScale, math.Max(minScale, math.Min(availableWidth/boundsWidth, availableHeight/boundsHeight)))
	centerX := (bounds.Left + bounds.Right) / 2
	centerY := (bounds.Top + bounds.Bottom) / 2
	viewport := Viewport{
		X: size.Width/2 - centerX*k,
		Y: size.Height/2 - centerY*k,
		K: k,
	}
	if err := validateViewport(viewport); err != nil {
		return Viewport{}, kernel.NewAppError(http.StatusBadRequest, "算出的视口超出允许范围")
	}
	return viewport, nil
}

func validateViewport(viewport Viewport) error {
	if !finiteCoord(viewport.X) || !finiteCoord(viewport.Y) || !finiteScale(viewport.K) {
		return kernel.NewAppError(http.StatusBadRequest, "视口 x、y 必须是有限坐标，k 必须在 0.05 到 8 之间")
	}
	return nil
}

func validViewportSize(size ViewportSize) bool {
	return finitePositive(size.Width) && finitePositive(size.Height) && size.Width <= 100000 && size.Height <= 100000
}

func validPadding(padding float64) bool {
	return !math.IsNaN(padding) && !math.IsInf(padding, 0) && padding >= 0 && padding <= 1e6
}

func validateBounds(bounds CanvasBounds) error {
	if !finiteCoord(bounds.Left) || !finiteCoord(bounds.Top) || !finiteCoord(bounds.Right) || !finiteCoord(bounds.Bottom) {
		return kernel.NewAppError(http.StatusBadRequest, "bbox 的 left、top、right、bottom 必须是有限坐标")
	}
	if bounds.Right < bounds.Left || bounds.Bottom < bounds.Top {
		return kernel.NewAppError(http.StatusBadRequest, "bbox 的 right/bottom 不能小于 left/top")
	}
	return nil
}

func finiteCoord(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Abs(value) <= maxViewportCoord
}

func finiteScale(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minViewportScale && value <= maxViewportScale
}

func finitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

func boundsForVisibleNodes(doc map[string]any) (CanvasBounds, int, error) {
	nodes := canvasNodeMaps(doc)
	byID := indexCanvasNodes(nodes)
	visible := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		if nodeHiddenFromFit(node, byID) {
			continue
		}
		visible = append(visible, node)
	}
	return unionNodeBounds(visible)
}

func boundsForNodeIDs(doc map[string]any, ids []string) (CanvasBounds, int, error) {
	if len(ids) == 0 || len(ids) > maxFitNodeIDs {
		return CanvasBounds{}, 0, kernel.NewAppError(http.StatusBadRequest, "nodeIds 必须是 1..200 项的数组")
	}
	nodes := canvasNodeMaps(doc)
	byID := indexCanvasNodes(nodes)
	seen := map[string]bool{}
	selected := make([]map[string]any, 0, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return CanvasBounds{}, 0, kernel.NewAppError(http.StatusBadRequest, "nodeIds 包含空 ID")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		node := byID[id]
		if node == nil {
			return CanvasBounds{}, 0, kernel.NewAppError(http.StatusNotFound, "节点不属于当前画布: "+id)
		}
		if nodeHiddenFromFit(node, byID) {
			continue
		}
		selected = append(selected, node)
	}
	return unionNodeBounds(selected)
}

func unionNodeBounds(nodes []map[string]any) (CanvasBounds, int, error) {
	var bounds CanvasBounds
	count := 0
	for _, node := range nodes {
		next, ok := canvasNodeBounds(node)
		if !ok {
			continue
		}
		if count == 0 {
			bounds = next
		} else {
			bounds.Left = math.Min(bounds.Left, next.Left)
			bounds.Top = math.Min(bounds.Top, next.Top)
			bounds.Right = math.Max(bounds.Right, next.Right)
			bounds.Bottom = math.Max(bounds.Bottom, next.Bottom)
		}
		count++
	}
	if count == 0 {
		return CanvasBounds{}, 0, kernel.NewAppError(http.StatusBadRequest, "没有可对准的节点")
	}
	return bounds, count, nil
}

func canvasNodeMaps(doc map[string]any) []map[string]any {
	raw, _ := doc["nodes"].([]any)
	nodes := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		node, ok := item.(map[string]any)
		if ok {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func indexCanvasNodes(nodes []map[string]any) map[string]map[string]any {
	byID := make(map[string]map[string]any, len(nodes))
	for _, node := range nodes {
		if id, _ := node["id"].(string); id != "" {
			byID[id] = node
		}
	}
	return byID
}

// nodeHiddenFromFit 对齐界面 fitCanvasContent：收起的批量子节点和折叠画框里的子节点不参与适应。
func nodeHiddenFromFit(node map[string]any, byID map[string]map[string]any) bool {
	meta, _ := node["metadata"].(map[string]any)
	if meta != nil {
		if rootID, _ := meta["batchRootId"].(string); strings.TrimSpace(rootID) != "" {
			if root := byID[rootID]; root != nil && !imageBatchExpanded(root) {
				return true
			}
		}
	}
	parentID, _ := node["parentId"].(string)
	parent := byID[strings.TrimSpace(parentID)]
	if parent == nil || parent["type"] != "frame" {
		return false
	}
	return frameCollapsed(parent)
}

func imageBatchExpanded(node map[string]any) bool {
	meta, _ := node["metadata"].(map[string]any)
	if meta == nil {
		return false
	}
	expanded, _ := meta["imageBatchExpanded"].(bool)
	return expanded
}

func frameCollapsed(node map[string]any) bool {
	meta, _ := node["metadata"].(map[string]any)
	if meta == nil {
		return false
	}
	frame, _ := meta["frame"].(map[string]any)
	if frame == nil {
		return false
	}
	collapsed, _ := frame["collapsed"].(bool)
	return collapsed
}

func canvasNodeBounds(node map[string]any) (CanvasBounds, bool) {
	position, _ := node["position"].(map[string]any)
	if position == nil {
		return CanvasBounds{}, false
	}
	x, xOK := floatOf(position["x"])
	y, yOK := floatOf(position["y"])
	if !xOK || !yOK || !finiteCoord(x) || !finiteCoord(y) {
		return CanvasBounds{}, false
	}
	width, widthOK := floatOf(node["width"])
	height, heightOK := floatOf(node["height"])
	if !widthOK || width <= 0 {
		width = defaultNodeWidth
	}
	if !heightOK || height <= 0 {
		height = defaultNodeHeight
	}
	return CanvasBounds{Left: x, Top: y, Right: x + width, Bottom: y + height}, true
}

func floatOf(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
