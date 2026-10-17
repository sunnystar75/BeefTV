package operations

import (
	"context"
	"strings"
	"sync"
	"time"
)

// DirectorPreviewDispatch 是一次已接受的白膜确认，等待打开中的桌面画布去录制。
// 录制仍在本机 WebGL 完成，这里不创建视频任务，也不调用上游模型。
type DirectorPreviewDispatch struct {
	UserID         string   `json:"-"`
	OperationID    string   `json:"operationId"`
	ProposalID     string   `json:"proposalId"`
	CanvasID       string   `json:"canvasId"`
	NodeIDs        []string `json:"nodeIds"`
	Kind           string   `json:"kind"`
	ModelKey       string   `json:"modelKey"`
	Charged        bool     `json:"charged"`
	CanvasRevision int64    `json:"canvasRevision"`
}

// DirectorDispatchHub 把确认结果交给同一进程里已打开的桌面界面。
// 外部 CLI 只调用确认；桌面轮询或短等待后自己跑 exportClayVideo。
type DirectorDispatchHub struct {
	mu      sync.Mutex
	pending []DirectorPreviewDispatch
	acked   map[string]struct{}
	waiters []directorDispatchWaiter
}

type directorDispatchWaiter struct {
	userID   string
	canvasID string
	notify   chan struct{}
}

var defaultDirectorDispatchHub = NewDirectorDispatchHub()

func NewDirectorDispatchHub() *DirectorDispatchHub {
	return &DirectorDispatchHub{acked: map[string]struct{}{}}
}

func DefaultDirectorDispatchHub() *DirectorDispatchHub { return defaultDirectorDispatchHub }

func ResetDirectorDispatchHub() {
	defaultDirectorDispatchHub = NewDirectorDispatchHub()
}

func publishDirectorPreviewDispatch(userID string, result any) {
	payload, _ := result.(map[string]any)
	if payload == nil || payload["kind"] != DirectorPreviewKind || payload["status"] != "accepted" {
		return
	}
	nodeIDs := dispatchStringList(payload["nodeIds"])
	canvasID, _ := payload["canvasId"].(string)
	proposalID, _ := payload["proposalId"].(string)
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(canvasID) == "" || strings.TrimSpace(proposalID) == "" || len(nodeIDs) == 0 {
		return
	}
	DefaultDirectorDispatchHub().Publish(DirectorPreviewDispatch{
		UserID:         userID,
		OperationID:    "confirm:" + strings.TrimSpace(proposalID),
		ProposalID:     strings.TrimSpace(proposalID),
		CanvasID:       strings.TrimSpace(canvasID),
		NodeIDs:        nodeIDs,
		Kind:           DirectorPreviewKind,
		ModelKey:       DirectorClayPreviewModelKey,
		Charged:        false,
		CanvasRevision: dispatchCanvasRevision(payload),
	})
}

func dispatchCanvasRevision(payload map[string]any) int64 {
	source, _ := payload["source"].(map[string]any)
	if source == nil {
		return 0
	}
	switch value := source["canvasRevision"].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	default:
		return 0
	}
}

func dispatchStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func (h *DirectorDispatchHub) Publish(item DirectorPreviewDispatch) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	key := dispatchAckKey(item.UserID, item.OperationID)
	if _, done := h.acked[key]; done {
		return
	}
	for _, existing := range h.pending {
		if dispatchAckKey(existing.UserID, existing.OperationID) == key {
			h.notifyLocked(item.UserID, item.CanvasID)
			return
		}
	}
	cloned := item
	cloned.NodeIDs = append([]string(nil), item.NodeIDs...)
	h.pending = append(h.pending, cloned)
	h.notifyLocked(item.UserID, item.CanvasID)
}

func (h *DirectorDispatchHub) Pending(userID, canvasID string) []DirectorPreviewDispatch {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pendingLocked(userID, canvasID)
}

func (h *DirectorDispatchHub) Wait(ctx context.Context, userID, canvasID string, timeout time.Duration) []DirectorPreviewDispatch {
	if h == nil {
		return nil
	}
	if items := h.Pending(userID, canvasID); len(items) > 0 || timeout <= 0 {
		return items
	}
	if ctx == nil {
		ctx = context.Background()
	}
	notify := make(chan struct{}, 1)
	h.mu.Lock()
	h.waiters = append(h.waiters, directorDispatchWaiter{userID: userID, canvasID: canvasID, notify: notify})
	h.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-notify:
	case <-timer.C:
	case <-ctx.Done():
	}
	h.mu.Lock()
	filtered := h.waiters[:0]
	for _, waiter := range h.waiters {
		if waiter.notify != notify {
			filtered = append(filtered, waiter)
		}
	}
	h.waiters = filtered
	items := h.pendingLocked(userID, canvasID)
	h.mu.Unlock()
	return items
}

func (h *DirectorDispatchHub) Ack(userID, canvasID, operationID string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	key := dispatchAckKey(userID, operationID)
	next := h.pending[:0]
	found := false
	for _, item := range h.pending {
		if item.UserID == userID && item.CanvasID == canvasID && item.OperationID == operationID {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		return false
	}
	h.pending = next
	h.acked[key] = struct{}{}
	return true
}

func (h *DirectorDispatchHub) pendingLocked(userID, canvasID string) []DirectorPreviewDispatch {
	out := make([]DirectorPreviewDispatch, 0)
	for _, item := range h.pending {
		if item.UserID == userID && item.CanvasID == canvasID {
			cloned := item
			cloned.NodeIDs = append([]string(nil), item.NodeIDs...)
			out = append(out, cloned)
		}
	}
	return out
}

func (h *DirectorDispatchHub) notifyLocked(userID, canvasID string) {
	for _, waiter := range h.waiters {
		if waiter.userID == userID && waiter.canvasID == canvasID {
			select {
			case waiter.notify <- struct{}{}:
			default:
			}
		}
	}
}

func dispatchAckKey(userID, operationID string) string {
	return strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(operationID)
}
