package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"infinite-canvas/backend/internal/agentops"
	"infinite-canvas/backend/internal/app"
)

// requireOwner 是所有 owner 级入口的唯一准入判断：本机同源 + owner 凭据。
// 统一在这里，避免每个文件各写一份表达式。
func requireOwner(c *gin.Context, svc *app.Service) bool {
	if !isLoopbackRequest(c.Request) {
		fail(c, http.StatusForbidden, app.BadAuthRequest("只接受本机同源请求"))
		return false
	}
	if !ownerCredentialTrusted(c, svc) {
		fail(c, http.StatusForbidden, app.BadAuthRequest("该操作需要 owner 凭据"))
		return false
	}
	return true
}

// ownerCredentialTrusted 判定 owner 级凭据是否被信任。
//
// 桌面形态（无 OrcaDock 身份）保持原有语义：本机 owner token 即可。OrcaTV
// 服务端形态下，owner token 绝不是成员绕过 OrcaDock 身份的旁路——只有已认证的
// 管理员才允许触发 owner 级能力，普通成员一律拒绝。
func ownerCredentialTrusted(c *gin.Context, svc *app.Service) bool {
	if !agentops.OwnerTokenMatches(svc.DataDir(), strings.TrimSpace(c.GetHeader("X-Beeftv-Owner"))) {
		return false
	}
	if principal, err := CurrentPrincipal(c); err == nil {
		return principal.IsAdmin()
	}
	return true
}
