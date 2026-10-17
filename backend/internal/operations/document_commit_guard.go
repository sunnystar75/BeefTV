package operations

import (
	"encoding/json"

	"infinite-canvas/backend/internal/canvas"
)

// guardCanvasDocumentReplace blocks external and owner-token CLI callers when
// document.nodes would shrink a multi-node canvas. Fine-grained ops never call
// this: they commit a document they already edited. Desktop manual saves do
// not set the guard, so ordinary editor deletes still save.
func guardCanvasDocumentReplace(ctx *Context, canvasID string, document json.RawMessage, allow bool) error {
	if ctx == nil || !ctx.Caller.guardsFullDocumentReplace() {
		return nil
	}
	current, err := ctx.Domain.UserCanvasProject(ctx.UserID, canvasID)
	if err != nil {
		return mapDomainError(err)
	}
	risk, err := canvas.AssessExternalNodeReplace(current, document, canvas.NodeReplaceOverlay)
	if err != nil {
		return mapDomainError(err)
	}
	if !risk.Blocked() {
		return nil
	}
	if !allow {
		canvas.LogExternalNodeReplace("commit", canvasID, risk, false)
		return PreconditionFailed(string(canvas.ExternalNodeReplaceReason), risk.Message(), risk.Details())
	}
	canvas.LogExternalNodeReplace("commit", canvasID, risk, true)
	return nil
}
