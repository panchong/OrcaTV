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

// trustedWorkspaceUI 是界面入口的准入并集：桌面形态走本机同源链，
// OrcaTV 托管形态走 OrcaDock 会话链。两者都不需要把 owner 凭据交给浏览器。
func trustedWorkspaceUI(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return isLoopbackRequest(c.Request) || hostedTrustedUIRequest(c)
}

// hostedTrustedUIRequest 判定请求是否来自受信任的 OrcaTV 托管界面。
//
// OrcaTV 形态没有桌面壳，也没有 loopback：请求由容器内的网关转发，源地址是容器
// 网络地址，浏览器只持有 HttpOnly 的 orcatv-session Cookie。这里的信任链因此是
// 「OrcaDock 会话中间件已装入 Principal（会话仍然有效）+ Origin 与 Host 同源」，
// 会话 Cookie 为 SameSite=Lax，跨站请求既不带凭据也会被同源判定拒绝。
// 普通成员的 owner 级能力由服务端按该 Principal 代理执行，owner 凭据不出服务端。
func hostedTrustedUIRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if _, err := CurrentPrincipal(c); err != nil {
		return false
	}
	return sameOriginRequest(c.Request)
}
