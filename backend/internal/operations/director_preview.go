package operations

import (
	"encoding/json"
	"strings"
)

const (
	// DirectorPreviewKind 是免费本地白膜导出的提议种类，不创建付费视频任务。
	DirectorPreviewKind = "director_preview"
	// DirectorClayPreviewModelKey 固定标识本机 WebGL 白膜录制，不对应任何上游模型。
	DirectorClayPreviewModelKey  = "local::director-clay-preview"
	directorClayPreviewModelName = "导演白膜"
)

func registerDirectorPreviewOps(r *Registry) {
	r.Register(Op{
		AssistantProposal: true, AssistantCanvas: true, ID: "director.shot.export_preview",
		Summary:  "提议导出导演台白膜视频（只登记免费本地提议，不录制、不扣费）",
		ReadOnly: true, Scope: ScopeCanvas,
		Params:  json.RawMessage(`{"type":"object","properties":{"canvasId":{"type":"string"},"nodeIds":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":8},"note":{"type":"string"}},"required":["canvasId","nodeIds"]}`),
		Handler: opDirectorShotExportPreview,
	})
	r.Register(Op{
		ReplaySafe: true, AssistantCanvas: true, ID: "canvas.generation.confirm",
		Summary: "确认一项生成或白膜提议。图片和视频不在这里创建付费任务；导演白膜通知已打开的桌面录制",
		Scope:   ScopeCanvas,
		Params:  json.RawMessage(`{"type":"object","properties":{"proposalId":{"type":"string"},"canvasId":{"type":"string"},"nodeIds":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":8},"kind":{"type":"string","enum":["image","video","director_preview"]},"modelKey":{"type":"string"},"charged":{"type":"boolean"},"note":{"type":"string"},"source":{"type":"object","properties":{"canvasId":{"type":"string"},"canvasRevision":{"type":"integer"},"modelConfigRevision":{"type":"integer"}},"required":["canvasId","canvasRevision"]}},"required":["proposalId","canvasId","nodeIds","kind","modelKey","source"]}`),
		Handler: opCanvasGenerationConfirm,
	})
}

func confirmEffectKey(params json.RawMessage) string {
	var args struct {
		ProposalID string `json:"proposalId"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return ""
	}
	proposalID := strings.TrimSpace(args.ProposalID)
	if proposalID == "" {
		return ""
	}
	return "confirm:" + proposalID
}

func opDirectorShotExportPreview(ctx *Context, params json.RawMessage) (any, error) {
	var args struct {
		CanvasID string   `json:"canvasId"`
		NodeIDs  []string `json:"nodeIds"`
		Note     string   `json:"note"`
	}
	if err := decodeParams(params, &args); err != nil {
		return nil, err
	}
	doc, nodeIDs, err := directorPreviewTargets(ctx, args.CanvasID, args.NodeIDs, DirectorPreviewKind)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"proposalId": newProposalID(),
		"kind":       DirectorPreviewKind,
		"nodeIds":    nodeIDs,
		"model":      directorClayPreviewModelName,
		"modelKey":   DirectorClayPreviewModelKey,
		"charged":    false,
		"local":      true,
		"note":       strings.TrimSpace(args.Note),
		"source": map[string]any{
			"canvasId":       strings.TrimSpace(args.CanvasID),
			"canvasRevision": canvasRevision(doc),
		},
	}, nil
}

type generationConfirmArgs struct {
	ProposalID string   `json:"proposalId"`
	CanvasID   string   `json:"canvasId"`
	NodeIDs    []string `json:"nodeIds"`
	Kind       string   `json:"kind"`
	ModelKey   string   `json:"modelKey"`
	Charged    *bool    `json:"charged"`
	Note       string   `json:"note"`
	Source     struct {
		CanvasID            string `json:"canvasId"`
		CanvasRevision      int64  `json:"canvasRevision"`
		ModelConfigRevision *int64 `json:"modelConfigRevision"`
	} `json:"source"`
}

func opCanvasGenerationConfirm(ctx *Context, params json.RawMessage) (any, error) {
	var args generationConfirmArgs
	if err := decodeParams(params, &args); err != nil {
		return nil, err
	}
	switch args.Kind {
	case DirectorPreviewKind:
		return confirmDirectorPreview(ctx, args)
	case "image", "video":
		return confirmPaidGeneration(ctx, args)
	default:
		return nil, InvalidArg("invalid_kind", "kind 只能是 image、video 或 director_preview")
	}
}

func confirmDirectorPreview(ctx *Context, args generationConfirmArgs) (any, error) {
	if strings.TrimSpace(args.ModelKey) != DirectorClayPreviewModelKey {
		return nil, InvalidArg("invalid_model", "导演白膜只使用本地模型 "+DirectorClayPreviewModelKey)
	}
	if args.Charged != nil && *args.Charged {
		return nil, InvalidArg("director_preview_not_billable", "导演白膜导出不计费")
	}
	doc, nodeIDs, err := confirmedGenerationTargets(ctx, args)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"proposalId":  strings.TrimSpace(args.ProposalID),
		"canvasId":    strings.TrimSpace(args.CanvasID),
		"nodeIds":     nodeIDs,
		"kind":        DirectorPreviewKind,
		"model":       directorClayPreviewModelName,
		"modelKey":    DirectorClayPreviewModelKey,
		"charged":     false,
		"local":       true,
		"status":      "accepted",
		"dispatched":  true,
		"taskCreated": false,
		"note":        strings.TrimSpace(args.Note),
		"source": map[string]any{
			"canvasId":       strings.TrimSpace(args.CanvasID),
			"canvasRevision": canvasRevision(doc),
		},
	}, nil
}

// confirmPaidGeneration 接受图片/视频确认，但不在这里创建任务。
// 付费生成仍由画布界面沿既有生成链路提交；这条操作只保证 kind 校验和幂等回执，避免白膜确认误走扣费。
func confirmPaidGeneration(ctx *Context, args generationConfirmArgs) (any, error) {
	if strings.TrimSpace(args.ModelKey) == "" {
		return nil, InvalidArg("invalid_model", "图片或视频确认必须带上提议里的模型")
	}
	if strings.TrimSpace(args.ModelKey) == DirectorClayPreviewModelKey {
		return nil, InvalidArg("generation_model_kind_mismatch", "本地白膜模型不能用来生成"+generationKindLabel(args.Kind))
	}
	_, nodeIDs, err := confirmedGenerationTargets(ctx, args)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"proposalId":  strings.TrimSpace(args.ProposalID),
		"canvasId":    strings.TrimSpace(args.CanvasID),
		"nodeIds":     nodeIDs,
		"kind":        args.Kind,
		"modelKey":    strings.TrimSpace(args.ModelKey),
		"status":      "accepted",
		"dispatched":  false,
		"taskCreated": false,
		"execution":   "canvas_generation",
		"note":        strings.TrimSpace(args.Note),
		"source": map[string]any{
			"canvasId":       strings.TrimSpace(args.Source.CanvasID),
			"canvasRevision": args.Source.CanvasRevision,
		},
	}
	if args.Charged != nil {
		result["charged"] = *args.Charged
	}
	if args.Source.ModelConfigRevision != nil {
		source := result["source"].(map[string]any)
		source["modelConfigRevision"] = *args.Source.ModelConfigRevision
	}
	return result, nil
}

func confirmedGenerationTargets(ctx *Context, args generationConfirmArgs) (map[string]any, []string, error) {
	if strings.TrimSpace(args.ProposalID) == "" {
		return nil, nil, InvalidArg("invalid_params", "proposalId 必填")
	}
	if strings.TrimSpace(args.Source.CanvasID) == "" || strings.TrimSpace(args.Source.CanvasID) != strings.TrimSpace(args.CanvasID) {
		return nil, nil, InvalidArg("canvas_mismatch", "确认来源画布与 canvasId 不一致")
	}
	doc, nodeIDs, err := directorPreviewTargets(ctx, args.CanvasID, args.NodeIDs, args.Kind)
	if err != nil {
		return nil, nil, err
	}
	if canvasRevision(doc) != args.Source.CanvasRevision {
		return nil, nil, PreconditionFailed("canvas_revision_conflict", "画布已经变化，请重新提出后再确认", map[string]any{
			"canvasRevision": canvasRevision(doc),
		})
	}
	return doc, nodeIDs, nil
}

func directorPreviewTargets(ctx *Context, canvasID string, requested []string, kind string) (map[string]any, []string, error) {
	if strings.TrimSpace(canvasID) == "" {
		return nil, nil, InvalidArg("invalid_params", "canvasId 必填")
	}
	if len(requested) == 0 || len(requested) > 8 {
		return nil, nil, InvalidArg("invalid_batch", "nodeIds 必须是 1..8 项的数组")
	}
	raw, err := ctx.Domain.UserCanvasProject(ctx.UserID, canvasID)
	if err != nil {
		return nil, nil, mapDomainError(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, AsError(err)
	}
	owned := map[string]map[string]any{}
	for _, rawNode := range canvasNodes(doc) {
		node, ok := rawNode.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := node["id"].(string); id != "" {
			owned[id] = node
		}
	}
	seen := map[string]bool{}
	nodeIDs := make([]string, 0, len(requested))
	for _, id := range requested {
		node, exists := owned[id]
		if !exists {
			return nil, nil, InvalidArg("node_not_in_canvas", "节点不属于当前画布: "+id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := validateConfirmNodeKind(node, kind); err != nil {
			return nil, nil, err
		}
		nodeIDs = append(nodeIDs, id)
	}
	return doc, nodeIDs, nil
}

func validateConfirmNodeKind(node map[string]any, kind string) error {
	if isDirectorNode(node) {
		if kind == DirectorPreviewKind {
			return nil
		}
		return InvalidArg("generation_kind_mismatch", "这个节点不能用来生成"+generationKindLabel(kind))
	}
	if kind == DirectorPreviewKind {
		return InvalidArg("generation_kind_mismatch", "这个节点不能用来导出白膜")
	}
	return validateProposalNodeKind(node, kind)
}

func isDirectorNode(node map[string]any) bool {
	nodeType, _ := node["type"].(string)
	return strings.EqualFold(strings.TrimSpace(nodeType), "director")
}
