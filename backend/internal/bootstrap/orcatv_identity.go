package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	canvasHandler "infinite-canvas/backend/internal/handler"
	"infinite-canvas/backend/internal/identity"
)

const (
	envHandoffSecret  = "ORCATV_HANDOFF_SECRET"
	envServiceKey     = "ORCATV_SERVICE_KEY"
	envIntrospectURL  = "ORCADOCK_INTROSPECT_URL"
	envLoginURL       = "ORCATV_LOGIN_URL"
	envCookieDomain   = "ORCATV_COOKIE_DOMAIN"
	envSessionTTL     = "ORCATV_SESSION_TTL"
	envSecureCookie   = "ORCATV_SECURE_COOKIE"
	envIntrospectWait = "ORCADOCK_INTROSPECT_TIMEOUT"
)

// orcaTVIdentity configures OrcaTV's OrcaDock-bound identity when the platform
// runs behind OrcaDock. It is enabled by the presence of a handoff secret; if
// any required peer setting is missing the runtime refuses to start rather
// than silently serving an unauthenticated or fixed-owner surface.
func orcaTVIdentity(cfg Config) (canvasHandler.OrcaTVIdentityOptions, bool, error) {
	secret := strings.TrimSpace(os.Getenv(envHandoffSecret))
	if secret == "" {
		return canvasHandler.OrcaTVIdentityOptions{}, false, nil
	}
	if len(secret) < 32 {
		return canvasHandler.OrcaTVIdentityOptions{}, false, errors.New(envHandoffSecret + " 至少需要 32 个字符")
	}
	serviceKey := strings.TrimSpace(os.Getenv(envServiceKey))
	if serviceKey == "" {
		return canvasHandler.OrcaTVIdentityOptions{}, false, errors.New(envServiceKey + " 未配置")
	}
	introspectURL := strings.TrimSpace(os.Getenv(envIntrospectURL))
	if introspectURL == "" {
		return canvasHandler.OrcaTVIdentityOptions{}, false, errors.New(envIntrospectURL + " 未配置")
	}
	timeout := 5 * time.Second
	if raw := strings.TrimSpace(os.Getenv(envIntrospectWait)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return canvasHandler.OrcaTVIdentityOptions{}, false, fmt.Errorf("%s 必须是正数时长，例如 5s", envIntrospectWait)
		}
		timeout = parsed
	}
	introspector, err := identity.NewHTTPIntrospector(introspectURL, serviceKey, timeout)
	if err != nil {
		return canvasHandler.OrcaTVIdentityOptions{}, false, err
	}
	ttl := identity.DefaultSessionTTL
	if raw := strings.TrimSpace(os.Getenv(envSessionTTL)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return canvasHandler.OrcaTVIdentityOptions{}, false, fmt.Errorf("%s 必须是正数时长，例如 24h", envSessionTTL)
		}
		ttl = parsed
	}
	secure := strings.EqualFold(strings.TrimSpace(os.Getenv(envSecureCookie)), "true")
	sessions, err := identity.NewService(identity.Config{
		DataDir: cfg.DataDir, HandoffSecret: []byte(secret), SessionTTL: ttl,
		Secure: secure, Introspector: introspector,
	})
	if err != nil {
		return canvasHandler.OrcaTVIdentityOptions{}, false, err
	}
	return canvasHandler.OrcaTVIdentityOptions{
		Sessions: sessions, ServiceKey: serviceKey, DataDir: cfg.DataDir,
		LoginURL:     strings.TrimSpace(os.Getenv(envLoginURL)),
		CookieDomain: strings.TrimSpace(os.Getenv(envCookieDomain)),
	}, true, nil
}
