package canvas

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/kernel"
)

func TestAssessExternalNodeReplaceMatchesIncidentPattern(t *testing.T) {
	current := json.RawMessage(`{"nodes":[{"id":"a"},{"id":"b"},{"id":"c"}]}`)
	partial := json.RawMessage(`{"nodes":[{"id":"b","type":"director"}]}`)
	risk, err := AssessExternalNodeReplace(current, partial, NodeReplaceOverlay)
	if err != nil {
		t.Fatal(err)
	}
	if !risk.Blocked() || risk.CurrentCount != 3 || risk.SubmittedCount != 1 || risk.DroppedCount != 2 {
		t.Fatalf("partial patch must be blocked: %+v", risk)
	}
	if !strings.Contains(risk.Message(), "CAS 覆盖") || !strings.Contains(risk.Message(), "allowFullDocumentReplace") {
		t.Fatalf("message must explain the full overwrite: %s", risk.Message())
	}
	appErr, ok := risk.Error().(*kernel.AppError)
	if !ok || appErr.Status != http.StatusPreconditionFailed || appErr.Reason != ExternalNodeReplaceReason {
		t.Fatalf("error = %#v", risk.Error())
	}

	kept, err := AssessExternalNodeReplace(current, json.RawMessage(`{"title":"只改标题"}`), NodeReplaceOverlay)
	if err != nil || kept.Blocked() {
		t.Fatalf("overlay without nodes must keep the canvas: %+v %v", kept, err)
	}
	full, err := AssessExternalNodeReplace(current, json.RawMessage(`{"nodes":[{"id":"a"},{"id":"c"},{"id":"b"},{"id":"new"}]}`), NodeReplaceOverlay)
	if err != nil || full.Blocked() {
		t.Fatalf("keeping every current id is not a wipe: %+v %v", full, err)
	}
	emptied, err := AssessExternalNodeReplace(current, json.RawMessage(`{"nodes":[]}`), NodeReplaceOverlay)
	if err != nil || !emptied.Blocked() || emptied.DroppedCount != 3 {
		t.Fatalf("empty nodes must be blocked: %+v %v", emptied, err)
	}
}

func TestAssessExternalNodeReplacePayloadAndShape(t *testing.T) {
	current := json.RawMessage(`{"nodes":[{"id":"a"},{"id":"b"}]}`)
	missing, err := AssessExternalNodeReplace(current, json.RawMessage(`{"title":"无节点字段"}`), NodeReplacePayload)
	if err != nil || !missing.Blocked() || missing.SubmittedCount != 0 || missing.DroppedCount != 2 {
		t.Fatalf("PUT without nodes drops the canvas: %+v %v", missing, err)
	}
	oneNode := json.RawMessage(`{"nodes":[{"id":"only"}]}`)
	swapped, err := AssessExternalNodeReplace(oneNode, json.RawMessage(`{"nodes":[{"id":"other"}]}`), NodeReplaceOverlay)
	if err != nil || swapped.Blocked() {
		t.Fatalf("replacing the only node is outside the multi-node guard: %+v %v", swapped, err)
	}
	cleared, err := AssessExternalNodeReplace(oneNode, json.RawMessage(`{"nodes":[]}`), NodeReplaceOverlay)
	if err != nil || !cleared.Blocked() {
		t.Fatalf("emptying the only node must be blocked: %+v %v", cleared, err)
	}
	_, err = AssessExternalNodeReplace(current, json.RawMessage(`{"nodes":{"id":"a"}}`), NodeReplaceOverlay)
	var appErr *kernel.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusBadRequest {
		t.Fatalf("non-array nodes must fail closed: %v", err)
	}
}
