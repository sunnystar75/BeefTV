package app

import (
	"encoding/json"
	"errors"

	"gorm.io/gorm"

	"infinite-canvas/backend/internal/canvas"
)

// GuardExternalCanvasPayloadReplace rejects an external PUT that would store
// a smaller node set. Desktop saves do not call this. allow is the explicit
// allowFullDocumentReplace flag and is never read from inside the project JSON.
func (s *Service) GuardExternalCanvasPayloadReplace(userID, canvasID string, project json.RawMessage, allow bool) error {
	current, err := s.UserCanvasProject(userID, canvasID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	risk, err := canvas.AssessExternalNodeReplace(current, project, canvas.NodeReplacePayload)
	if err != nil {
		return err
	}
	if !risk.Blocked() {
		return nil
	}
	if !allow {
		canvas.LogExternalNodeReplace("put", canvasID, risk, false)
		return risk.Error()
	}
	canvas.LogExternalNodeReplace("put", canvasID, risk, true)
	return nil
}
