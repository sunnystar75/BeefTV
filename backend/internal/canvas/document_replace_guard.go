package canvas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/kernel"
)

// ExternalNodeReplaceReason is the stable reason for a refused full-document
// node wipe. Clients must branch on this value, not on the message text.
const ExternalNodeReplaceReason kernel.ErrorReason = "full_document_replace_required"

// NodeReplaceMode selects how a missing nodes key is interpreted.
type NodeReplaceMode int

const (
	// NodeReplaceOverlay matches canvas.document.commit: omitted top-level keys,
	// including nodes, stay as they are on the current document.
	NodeReplaceOverlay NodeReplaceMode = iota
	// NodeReplacePayload matches PUT /canvas-projects/:id: the submitted JSON
	// becomes the stored document, so a missing nodes key removes every node.
	NodeReplacePayload
)

// ExternalNodeReplace is the node-set effect of a whole-document write.
// Counts are node array lengths. DroppedCount is how many current node ids
// would disappear; an empty submission reports the whole current canvas.
type ExternalNodeReplace struct {
	CurrentCount   int
	SubmittedCount int
	DroppedCount   int
}

// Blocked is the 2026-10-17 failure mode: a multi-node canvas would lose nodes,
// or any non-empty canvas would be emptied. Keeping every current node id
// (in-place edits or appends) is not blocked. Replacing the only node on a
// one-node canvas is left alone; that is not the partial-patch wipe.
func (r ExternalNodeReplace) Blocked() bool {
	if r.CurrentCount <= 0 {
		return false
	}
	if r.SubmittedCount == 0 {
		return true
	}
	return r.CurrentCount >= 2 && (r.DroppedCount > 0 || r.SubmittedCount < r.CurrentCount)
}

// Message tells the caller this write is a full CAS overwrite, not an upsert.
func (r ExternalNodeReplace) Message() string {
	return fmt.Sprintf(
		"这是整份画布文档的 CAS 覆盖，不是局部 upsert。提交的 nodes 会替换画布上的全部节点（当前 %d 个，提交 %d 个，将删除 %d 个）。canvas.document.commit 与 PUT /canvas-projects/:id 都按整份正文覆盖；绑定或修改节点请改用 canvas.nodes.create、canvas.node.update、canvas.node.configure、canvas.node.move、canvas.node.delete 或 canvas.edge.*。若确要整布替换，请在参数顶层显式传入 allowFullDocumentReplace: true（不要写进 document 或 project 正文）。",
		r.CurrentCount, r.SubmittedCount, r.DroppedCount,
	)
}

// Details is the machine-readable companion to Message.
func (r ExternalNodeReplace) Details() map[string]any {
	return map[string]any{
		"currentNodeCount":   r.CurrentCount,
		"submittedNodeCount": r.SubmittedCount,
		"droppedNodeCount":   r.DroppedCount,
	}
}

// Error is the HTTP/domain error for a blocked replace. A non-blocked value
// returns nil so callers can use it as the guard result.
func (r ExternalNodeReplace) Error() error {
	if !r.Blocked() {
		return nil
	}
	err := kernel.NewAppError(http.StatusPreconditionFailed, r.Message())
	err.Reason = ExternalNodeReplaceReason
	return err
}

// LogExternalNodeReplace records a blocked full replace without document
// contents, prompts, URLs, or credentials.
func LogExternalNodeReplace(action, canvasID string, risk ExternalNodeReplace, allowed bool) {
	log.Printf("canvas_document_replace action=%s canvas=%s nodes_before=%d nodes_after=%d dropped=%d allow_full_document_replace=%t",
		action, canvasID, risk.CurrentCount, risk.SubmittedCount, risk.DroppedCount, allowed)
}

// AssessExternalNodeReplace compares the current canvas document with a
// submitted document. A nodes value that is not an array is an error, so a
// malformed payload cannot skip the guard.
func AssessExternalNodeReplace(current, submitted json.RawMessage, mode NodeReplaceMode) (ExternalNodeReplace, error) {
	currentFields, err := documentObject(current)
	if err != nil {
		return ExternalNodeReplace{}, err
	}
	submittedFields, err := documentObject(submitted)
	if err != nil {
		return ExternalNodeReplace{}, err
	}
	currentCount, currentIDs, err := nodeIDSet(currentFields["nodes"], true)
	if err != nil {
		return ExternalNodeReplace{}, err
	}
	rawNodes, present := submittedFields["nodes"]
	if !present {
		if mode == NodeReplaceOverlay {
			return ExternalNodeReplace{}, nil
		}
		return ExternalNodeReplace{CurrentCount: currentCount, SubmittedCount: 0, DroppedCount: currentCount}, nil
	}
	submittedCount, submittedIDs, err := nodeIDSet(rawNodes, false)
	if err != nil {
		return ExternalNodeReplace{}, err
	}
	dropped := 0
	for id := range currentIDs {
		if _, ok := submittedIDs[id]; !ok {
			dropped++
		}
	}
	if submittedCount == 0 && dropped == 0 {
		dropped = currentCount
	}
	return ExternalNodeReplace{CurrentCount: currentCount, SubmittedCount: submittedCount, DroppedCount: dropped}, nil
}

func documentObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, kernel.BadAuthRequest("画布文档格式错误")
	}
	return fields, nil
}

func nodeIDSet(raw json.RawMessage, absentOK bool) (int, map[string]struct{}, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		if absentOK {
			return 0, map[string]struct{}{}, nil
		}
		return 0, nil, kernel.BadAuthRequest("画布 nodes 必须是数组")
	}
	if string(trimmed) == "null" {
		return 0, nil, kernel.BadAuthRequest("画布 nodes 必须是数组")
	}
	var nodes []json.RawMessage
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return 0, nil, kernel.BadAuthRequest("画布 nodes 必须是数组")
	}
	ids := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		node = bytes.TrimSpace(node)
		if len(node) == 0 || string(node) == "null" {
			continue
		}
		var header struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(node, &header); err != nil {
			return 0, nil, kernel.BadAuthRequest("画布 nodes 必须是对象数组")
		}
		if id := strings.TrimSpace(header.ID); id != "" {
			ids[id] = struct{}{}
		}
	}
	return len(nodes), ids, nil
}
