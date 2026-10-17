package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"infinite-canvas/backend/internal/app"
	"infinite-canvas/backend/internal/operations"
)

// RegisterDirectorPreviewDispatchRoutes 让已打开的桌面画布领取白膜确认。
// 外部客户端确认走 /ops，不经过这里，也不需要桌面启动令牌。
func RegisterDirectorPreviewDispatchRoutes(r gin.IRouter, svc *app.Service) {
	r.GET("/desktop/director-preview-dispatches", func(c *gin.Context) {
		user, canvasID, allowed := directorPreviewDesktopCanvas(c, svc)
		if !allowed {
			return
		}
		waitMs, _ := strconv.Atoi(c.Query("waitMs"))
		if waitMs < 0 {
			waitMs = 0
		}
		if waitMs > 3000 {
			waitMs = 3000
		}
		items := operations.DefaultDirectorDispatchHub().Wait(c.Request.Context(), user, canvasID, time.Duration(waitMs)*time.Millisecond)
		ok(c, gin.H{"dispatches": items})
	})
	r.POST("/desktop/director-preview-dispatches/ack", func(c *gin.Context) {
		user, _, allowed := directorPreviewDesktopCanvas(c, svc)
		if !allowed {
			return
		}
		var body struct {
			CanvasID    string `json:"canvasId"`
			OperationID string `json:"operationId"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			fail(c, http.StatusBadRequest, app.BadAuthRequest("请求体无效"))
			return
		}
		if strings.TrimSpace(body.CanvasID) == "" || strings.TrimSpace(body.OperationID) == "" {
			fail(c, http.StatusBadRequest, app.BadAuthRequest("canvasId 与 operationId 必填"))
			return
		}
		if _, err := svc.UserCanvasProject(user, strings.TrimSpace(body.CanvasID)); err != nil {
			failService(c, err)
			return
		}
		acked := operations.DefaultDirectorDispatchHub().Ack(user, strings.TrimSpace(body.CanvasID), strings.TrimSpace(body.OperationID))
		ok(c, gin.H{"acked": acked})
	})
}

func directorPreviewDesktopCanvas(c *gin.Context, svc *app.Service) (string, string, bool) {
	if !isLoopbackRequest(c.Request) {
		fail(c, http.StatusForbidden, app.BadAuthRequest("桌面通知只接受本机请求"))
		return "", "", false
	}
	user, err := currentUser(c, svc)
	if err != nil {
		failService(c, err)
		return "", "", false
	}
	canvasID := strings.TrimSpace(c.Query("canvasId"))
	if canvasID == "" {
		canvasID = strings.TrimSpace(c.PostForm("canvasId"))
	}
	if c.Request.Method == http.MethodPost && canvasID == "" {
		return user.ID, "", true
	}
	if canvasID == "" {
		fail(c, http.StatusBadRequest, app.BadAuthRequest("canvasId 必填"))
		return "", "", false
	}
	if _, err := svc.UserCanvasProject(user.ID, canvasID); err != nil {
		failService(c, err)
		return "", "", false
	}
	return user.ID, canvasID, true
}
