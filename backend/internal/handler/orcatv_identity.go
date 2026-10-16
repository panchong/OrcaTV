package handler

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/identity"
	"infinite-canvas/backend/internal/workspace"

	"github.com/gin-gonic/gin"
)

const principalContextKey = "orcatv.principal"

// loginURLHeader carries the OrcaDock entry point back to the OrcaTV frontend.
// It is only ever set on responses produced by the OrcaTV identity layer, so the
// frontend can distinguish "OrcaDock session is gone" from any other 401.
const loginURLHeader = "X-OrcaTV-Login"

// OrcaTVIdentityOptions wires the OrcaTV-specific identity surface.
type OrcaTVIdentityOptions struct {
	// Sessions issues and validates OrcaTV sessions.
	Sessions *identity.Service
	// ServiceKey guards the server-to-server revoke endpoint.
	ServiceKey string
	// DataDir is the platform data root; per-user workspace directories hang off it.
	DataDir string
	// LoginURL is where a browser is sent when its session is gone.
	LoginURL string
	// CookieDomain is optional; empty means host-only.
	CookieDomain string
}

// RegisterOrcaTVHealthRoutes exposes the public liveness probe. It is mounted
// outside the authenticated API group.
func RegisterOrcaTVHealthRoutes(router *gin.Engine) {
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
}

// RegisterOrcaTVIdentityRoutes mounts the handoff callback and the
// server-to-server revoke endpoint. Both must stay outside the authenticated
// API group: the handoff is how a session is created, and revoke is called by
// OrcaDock itself.
func RegisterOrcaTVIdentityRoutes(router *gin.Engine, options OrcaTVIdentityOptions) {
	router.GET("/auth/handoff", func(c *gin.Context) {
		if options.Sessions == nil {
			c.Redirect(http.StatusFound, options.loginRedirect("unavailable"))
			return
		}
		raw := strings.TrimSpace(c.Query("token"))
		claims, err := options.Sessions.AcceptHandoff(raw, time.Now().UTC())
		if err != nil {
			c.Redirect(http.StatusFound, options.loginRedirect("session"))
			return
		}
		cookieValue, _, err := options.Sessions.IssueSession(claims, time.Now().UTC())
		if err != nil {
			c.Redirect(http.StatusFound, options.loginRedirect("unavailable"))
			return
		}
		// The handoff token lives only in this URL; never let it leak downstream.
		c.Header("Referrer-Policy", "no-referrer")
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(identity.CookieName, cookieValue, int(options.Sessions.SessionTTL()/time.Second), "/", options.CookieDomain, options.Sessions.Secure(), true)
		// Immediately bounce to a clean address so the token cannot be bookmarked,
		// replayed from history, or captured by an analytics beacon.
		c.Redirect(http.StatusFound, "/")
	})

	router.POST("/internal/sessions/revoke", func(c *gin.Context) {
		if !serviceKeyMatches(options.ServiceKey, bearerToken(c.GetHeader("Authorization"))) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": "服务凭据无效"})
			return
		}
		if options.Sessions == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "msg": "OrcaTV 身份服务未就绪"})
			return
		}
		var body struct {
			SID    string `json:"sid"`
			UserID string `json:"userId"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "请求格式错误"})
			return
		}
		revoked := 0
		if sid := strings.TrimSpace(body.SID); sid != "" {
			revoked += options.Sessions.RevokeBySID(sid)
		}
		if userID := strings.TrimSpace(body.UserID); userID != "" {
			revoked += options.Sessions.RevokeByUser(userID)
		}
		if strings.TrimSpace(body.SID) == "" && strings.TrimSpace(body.UserID) == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "sid 或 userId 必填"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"revoked": revoked})
	})
}

// OrcaTVIdentityMiddleware is the single authenticated entrypoint for OrcaTV
// business routes. It rejects any request that is not backed by a live
// OrcaDock session and installs both the Principal and the per-user workspace
// scope on the request context.
func OrcaTVIdentityMiddleware(options OrcaTVIdentityOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		if options.Sessions == nil {
			options.abortSessionRequired(c)
			return
		}
		cookieValue, err := c.Cookie(identity.CookieName)
		if err != nil || strings.TrimSpace(cookieValue) == "" {
			options.abortSessionRequired(c)
			return
		}
		principal, err := options.Sessions.Authenticate(c.Request.Context(), cookieValue)
		if err != nil {
			options.abortSessionRequired(c)
			return
		}
		c.Set(principalContextKey, principal)
		c.Set(workspaceContextKey, WorkspaceForPrincipal(options.DataDir, principal.UserID))
		c.Next()
	}
}

// WorkspaceForPrincipal derives the per-user ownership scope. The workspace id
// is the OrcaDock userId; there is no shared owner workspace.
func WorkspaceForPrincipal(dataDir, userID string) workspace.Context {
	scope := workspace.Context{ID: strings.TrimSpace(userID), DataDir: strings.TrimSpace(dataDir)}
	if scope.ID != "" {
		scope.DataDir = filepath.Join(dataDir, "users", scope.ID)
	}
	return scope
}

// CurrentPrincipal returns the authenticated caller for this request.
func CurrentPrincipal(c *gin.Context) (identity.Principal, error) {
	value, exists := c.Get(principalContextKey)
	if !exists {
		return identity.Principal{}, identity.ErrUnauthenticated
	}
	principal, ok := value.(identity.Principal)
	if !ok {
		return identity.Principal{}, identity.ErrUnauthenticated
	}
	if err := principal.Validate(); err != nil {
		return identity.Principal{}, err
	}
	return principal, nil
}

// RequireAdmin rejects non-admin callers. It is used by platform-level
// configuration routes such as the shared model channel settings.
func RequireAdmin(c *gin.Context) (identity.Principal, bool) {
	principal, err := CurrentPrincipal(c)
	if err != nil {
		abortSessionRequired(c)
		return identity.Principal{}, false
	}
	if !principal.IsAdmin() {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "msg": "该操作仅限管理员"})
		return identity.Principal{}, false
	}
	return principal, true
}

// abortSessionRequired reports an unusable OrcaDock-backed session and tells
// the browser where to re-authenticate. Failures never degrade to a default
// user; the caller must return immediately after this.
func (o OrcaTVIdentityOptions) abortSessionRequired(c *gin.Context) {
	if login := strings.TrimSpace(o.LoginURL); login != "" {
		c.Header(loginURLHeader, login)
	}
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": "会话已失效，请重新登录 OrcaDock"})
}

func abortSessionRequired(c *gin.Context) {
	OrcaTVIdentityOptions{}.abortSessionRequired(c)
}

func (o OrcaTVIdentityOptions) loginRedirect(reason string) string {
	target := strings.TrimSpace(o.LoginURL)
	if target == "" {
		return "/"
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	query := parsed.Query()
	query.Set("orcatv", reason)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func bearerToken(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 7 && strings.EqualFold(value[:7], "bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return value
}

func serviceKeyMatches(expected, presented string) bool {
	expected = strings.TrimSpace(expected)
	presented = strings.TrimSpace(presented)
	if expected == "" || presented == "" || len(expected) != len(presented) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) == 1
}

// ErrPrincipalRequired is returned by helpers that must run inside an
// authenticated request.
var ErrPrincipalRequired = errors.New("需要已认证的 OrcaTV 会话")
