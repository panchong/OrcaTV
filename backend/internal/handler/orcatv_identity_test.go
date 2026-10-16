package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/identity"

	"github.com/gin-gonic/gin"
)

type handlerIntrospectorFunc func(context.Context, string) (identity.Principal, error)

func (f handlerIntrospectorFunc) Introspect(ctx context.Context, sid string) (identity.Principal, error) {
	return f(ctx, sid)
}

func newHandlerIdentityFixture(t *testing.T, introspector identity.Introspector) (*identity.Service, []byte, string) {
	t.Helper()
	dataDir := t.TempDir()
	secret := []byte("handler-test-secret-0123456789abcdef")
	service, err := identity.NewService(identity.Config{
		DataDir:       dataDir,
		HandoffSecret: secret,
		SessionTTL:    time.Hour,
		Introspector:  introspector,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, secret, dataDir
}

func issueHandlerSession(t *testing.T, service *identity.Service, secret []byte, now time.Time, sid string) string {
	t.Helper()
	raw, err := identity.SignHandoff(secret, identity.HandoffClaims{
		Issuer: identity.HandoffIssuer, Audience: identity.HandoffAudience,
		UserID: "u_alice", Role: identity.RoleAdmin, DisplayName: "Alice",
		SID: sid, JTI: "handler-test-jti-" + sid, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.AcceptHandoff(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	cookie, _, err := service.IssueSession(claims, now)
	if err != nil {
		t.Fatal(err)
	}
	return cookie
}

func TestOrcaTVIdentityMiddlewareFailsClosedWhenOrcaDockIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, secret, dataDir := newHandlerIdentityFixture(t, handlerIntrospectorFunc(func(context.Context, string) (identity.Principal, error) {
		return identity.Principal{}, identity.ErrUnauthenticated
	}))
	cookie := issueHandlerSession(t, service, secret, time.Now().UTC(), "sid-alice")

	router := gin.New()
	router.Use(OrcaTVIdentityMiddleware(OrcaTVIdentityOptions{
		Sessions: service, DataDir: dataDir, LoginURL: "http://127.0.0.1:3010/?from=orcatv",
	}))
	router.GET("/protected", func(c *gin.Context) {
		principal, err := CurrentPrincipal(c)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.JSON(http.StatusOK, gin.H{"userId": principal.UserID})
	})

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.AddCookie(&http.Cookie{Name: identity.CookieName, Value: cookie})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if got := response.Header().Get(loginURLHeader); got == "" {
		t.Fatal("login redirect header missing")
	}
	if !strings.Contains(response.Body.String(), "会话已失效") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}

	anonymous := httptest.NewRecorder()
	router.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", anonymous.Code)
	}
}

func TestOrcaTVHandoffSetsSessionCookieAndConsumesTokenOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, secret, dataDir := newHandlerIdentityFixture(t, handlerIntrospectorFunc(func(_ context.Context, sid string) (identity.Principal, error) {
		return identity.Principal{UserID: "u_alice", Role: identity.RoleAdmin, DisplayName: "Alice", SID: sid}, nil
	}))
	now := time.Now().UTC()
	raw, err := identity.SignHandoff(secret, identity.HandoffClaims{
		Issuer: identity.HandoffIssuer, Audience: identity.HandoffAudience,
		UserID: "u_alice", Role: identity.RoleAdmin, DisplayName: "Alice",
		SID: "sid-alice", JTI: "handoff-once", ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	RegisterOrcaTVIdentityRoutes(router, OrcaTVIdentityOptions{
		Sessions: service, DataDir: dataDir, LoginURL: "http://127.0.0.1:3010/",
	})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/auth/handoff?token="+raw, nil))
	if first.Code != http.StatusFound || first.Header().Get("Location") != "/" {
		t.Fatalf("handoff response = %d %q", first.Code, first.Header().Get("Location"))
	}
	setCookie := strings.ToLower(first.Header().Get("Set-Cookie"))
	for _, part := range []string{identity.CookieName + "=", "httponly", "samesite=lax", "path=/"} {
		if !strings.Contains(setCookie, part) {
			t.Fatalf("Set-Cookie missing %q: %s", part, setCookie)
		}
	}

	replay := httptest.NewRecorder()
	router.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, "/auth/handoff?token="+raw, nil))
	if replay.Code != http.StatusFound || !strings.Contains(replay.Header().Get("Location"), "orcatv=session") {
		t.Fatalf("replayed handoff response = %d %q", replay.Code, replay.Header().Get("Location"))
	}
}

func TestOrcaTVRevokeRequiresServiceKeyAndDropsSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, secret, dataDir := newHandlerIdentityFixture(t, handlerIntrospectorFunc(func(_ context.Context, sid string) (identity.Principal, error) {
		return identity.Principal{UserID: "u_alice", Role: identity.RoleAdmin, DisplayName: "Alice", SID: sid}, nil
	}))
	now := time.Now().UTC()
	cookie := issueHandlerSession(t, service, secret, now, "sid-alice")
	if _, err := service.Authenticate(context.Background(), cookie); err != nil {
		t.Fatalf("expected live session: %v", err)
	}

	router := gin.New()
	RegisterOrcaTVIdentityRoutes(router, OrcaTVIdentityOptions{
		Sessions: service, ServiceKey: "service-key-0123456789abcdef", DataDir: dataDir,
		LoginURL: "http://127.0.0.1:3010/",
	})

	denied := httptest.NewRecorder()
	deniedRequest := httptest.NewRequest(http.MethodPost, "/internal/sessions/revoke", strings.NewReader(`{"sid":"sid-alice"}`))
	deniedRequest.Header.Set("Content-Type", "application/json")
	deniedRequest.Header.Set("Authorization", "Bearer wrong-key")
	router.ServeHTTP(denied, deniedRequest)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("wrong service key status = %d, want 401", denied.Code)
	}

	revoked := httptest.NewRecorder()
	revokeRequest := httptest.NewRequest(http.MethodPost, "/internal/sessions/revoke", strings.NewReader(`{"sid":"sid-alice"}`))
	revokeRequest.Header.Set("Content-Type", "application/json")
	revokeRequest.Header.Set("Authorization", "Bearer service-key-0123456789abcdef")
	router.ServeHTTP(revoked, revokeRequest)
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, body=%s", revoked.Code, revoked.Body.String())
	}
	if _, err := service.Authenticate(context.Background(), cookie); err == nil {
		t.Fatal("revoked session still authenticates")
	}
}
