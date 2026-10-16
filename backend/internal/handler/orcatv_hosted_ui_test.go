package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"infinite-canvas/backend/internal/agentops"
	"infinite-canvas/backend/internal/identity"
	"infinite-canvas/backend/internal/model"
)

func issueHostedSession(t *testing.T, service *identity.Service, secret []byte, now time.Time, userID, role string) string {
	t.Helper()
	raw, err := identity.SignHandoff(secret, identity.HandoffClaims{
		Issuer: identity.HandoffIssuer, Audience: identity.HandoffAudience,
		UserID: userID, Role: role, DisplayName: "Hosted Member",
		SID: "sid-" + userID, JTI: "hosted-host-test-" + userID, ExpiresAt: now.Add(time.Minute),
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

func newHostedUIRequest(method, path, origin, cookie, body string) *http.Request {
	request := httptest.NewRequest(method, "http://127.0.0.1:3020"+path, strings.NewReader(body))
	request.Host = "127.0.0.1:3020"
	// nginx forwards the request to the backend, so the backend sees the
	// container network address rather than the browser's loopback address.
	request.RemoteAddr = "192.168.166.1:43120"
	request.Header.Set("Content-Type", "application/json")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: identity.CookieName, Value: cookie})
	}
	return request
}

func TestHostedTrustedUIRequiresAuthenticatedPrincipalAndSameOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	principal := identity.Principal{UserID: "u_member", Role: identity.RoleUser, DisplayName: "Member", SID: "sid-member"}

	cases := []struct {
		name      string
		principal *identity.Principal
		origin    string
		want      bool
	}{
		{"authenticated same-origin hosted UI", &principal, "http://127.0.0.1:3020", true},
		{"missing principal", nil, "http://127.0.0.1:3020", false},
		{"cross-origin hosted UI", &principal, "https://attacker.example", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = newHostedUIRequest(http.MethodGet, "/api/ops", tc.origin, "", "")
			if tc.principal != nil {
				context.Set(principalContextKey, *tc.principal)
			}
			if got := hostedTrustedUIRequest(context); got != tc.want {
				t.Fatalf("hostedTrustedUIRequest=%v want %v", got, tc.want)
			}
			if got := trustedWorkspaceUI(context); got != tc.want {
				t.Fatalf("trustedWorkspaceUI=%v want %v", got, tc.want)
			}
		})
	}
}

func TestHostedOrcaTVUISessionCanCommitCanvasAsOrdinaryMember(t *testing.T) {
	env := newAssistantTestEnv(t, nil)
	owner, err := env.service.LocalWorkspaceOwner()
	if err != nil {
		t.Fatal(err)
	}
	identityService, secret, dataDir := newHandlerIdentityFixture(t, handlerIntrospectorFunc(func(_ context.Context, sid string) (identity.Principal, error) {
		return identity.Principal{UserID: owner.ID, Role: identity.RoleUser, DisplayName: "Member", SID: sid}, nil
	}))
	cookie := issueHostedSession(t, identityService, secret, time.Now().UTC(), owner.ID, identity.RoleUser)

	router := gin.New()
	router.Use(OrcaTVIdentityMiddleware(OrcaTVIdentityOptions{Sessions: identityService, DataDir: dataDir, LoginURL: "http://127.0.0.1:3010/"}))
	api := router.Group("/api")
	RegisterAgentOpsRoutes(api, env.service, agentops.NewStore(env.service.Database()), env.clients)

	request := newHostedUIRequest(http.MethodPost, "/api/ops/canvas.document.commit", "http://127.0.0.1:3020", cookie,
		`{"opId":"hosted-doc-1","params":{"canvasId":"`+env.canvasID+`","expectedRevision":1,"document":{"title":"成员提交","nodes":[{"id":"n1","type":"text","title":"成员"}],"connections":[]}}}`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("托管成员提交画布失败: %d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Caller string `json:"caller"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Caller != string(agentops.CallerManual) {
		t.Fatalf("托管界面调用者=%q，期望 manual: %s", envelope.Data.Caller, response.Body.String())
	}
	var receipt model.AgentOpRecord
	if err := env.service.Database().Where("user_id = ? AND op_id = ?", owner.ID, "hosted-doc-1").First(&receipt).Error; err != nil {
		t.Fatalf("未按 Principal 用户写入操作回执: %v", err)
	}
	var forged int64
	if err := env.service.Database().Model(&model.AgentOpRecord{}).Where("user_id = ?", "forged-user").Count(&forged).Error; err != nil {
		t.Fatal(err)
	}
	if forged != 0 {
		t.Fatalf("画布提交错误归入伪造用户: %d", forged)
	}
}

func TestHostedOrcaTVOrdinaryMemberCanIssueAssistantUISession(t *testing.T) {
	env := newAssistantTestEnv(t, nil)
	owner, err := env.service.LocalWorkspaceOwner()
	if err != nil {
		t.Fatal(err)
	}
	identityService, secret, dataDir := newHandlerIdentityFixture(t, handlerIntrospectorFunc(func(_ context.Context, sid string) (identity.Principal, error) {
		return identity.Principal{UserID: owner.ID, Role: identity.RoleUser, DisplayName: "Member", SID: sid}, nil
	}))
	cookie := issueHostedSession(t, identityService, secret, time.Now().UTC(), owner.ID, identity.RoleUser)

	router := gin.New()
	router.Use(OrcaTVIdentityMiddleware(OrcaTVIdentityOptions{Sessions: identityService, DataDir: dataDir, LoginURL: "http://127.0.0.1:3010/"}))
	api := router.Group("/api")
	ui := newUISessionStore()
	RegisterAgentUISessionRoutes(api, env.service, ui, nil)

	request := newHostedUIRequest(http.MethodPost, "/api/assistant/ui-session", "http://127.0.0.1:3020", cookie, `{}`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("托管成员签发助手会话失败: %d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Token     string `json:"token"`
			Bootstrap struct {
				HostedUI   bool `json:"hostedUI"`
				OwnerToken bool `json:"ownerToken"`
			} `json:"bootstrap"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Token == "" || !envelope.Data.Bootstrap.HostedUI || envelope.Data.Bootstrap.OwnerToken {
		t.Fatalf("托管 UI 会话响应不符合预期: %s", response.Body.String())
	}
}
