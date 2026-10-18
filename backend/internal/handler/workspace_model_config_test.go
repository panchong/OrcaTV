package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"infinite-canvas/backend/internal/app"
	"infinite-canvas/backend/internal/identity"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/workspace"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWorkspaceModelConfigReturnsBuiltinBeefAPIAndPersistenceMetadata(t *testing.T) {
	router, _ := newModelConfigTestRouter(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/workspace/model-config", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	data := modelConfigResponseData(t, recorder)
	if data["health"] != string(workspace.ConfigHealthDefault) || data["revision"] != float64(0) || data["source"] != "builtin+local" {
		t.Fatalf("missing persistence metadata: %#v", data)
	}
	config, _ := data["config"].(map[string]any)
	channels, _ := config["channels"].([]any)
	if len(channels) != 1 || channels[0].(map[string]any)["id"] != "beefapi" {
		t.Fatalf("builtin BeefAPI missing: %#v", channels)
	}
	if models, _ := channels[0].(map[string]any)["models"].([]any); len(models) != 0 {
		t.Fatalf("model catalog must remain dynamic: %#v", models)
	}
}

func TestWorkspaceModelConfigGETRedactsBeefAPIKey(t *testing.T) {
	router, _ := newModelConfigTestRouter(t)
	first := putModelConfig(t, router, 0, "local-secret-key")
	if first.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", first.Code, first.Body.String())
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/workspace/model-config", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("local-secret-key")) {
		t.Fatalf("model-config leaked api key: %s", recorder.Body.String())
	}
	stale := putModelConfig(t, router, 1, "ui-overwrite")
	if stale.Code != http.StatusOK {
		t.Fatalf("second put status = %d body=%s", stale.Code, stale.Body.String())
	}
}

func TestWorkspaceModelConfigRejectsStaleRevisionWithoutLeakingSecrets(t *testing.T) {
	router, _ := newModelConfigTestRouter(t)
	first := putModelConfig(t, router, 0, "first-secret")
	if first.Code != http.StatusOK {
		t.Fatalf("first put status = %d body=%s", first.Code, first.Body.String())
	}
	if revision := modelConfigResponseData(t, first)["revision"]; revision != float64(1) {
		t.Fatalf("committed revision = %#v", revision)
	}

	stale := putModelConfig(t, router, 0, "must-not-leak")
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale put status = %d body=%s", stale.Code, stale.Body.String())
	}
	if bytes.Contains(stale.Body.Bytes(), []byte("must-not-leak")) || bytes.Contains(stale.Body.Bytes(), []byte("first-secret")) {
		t.Fatalf("error response leaked a secret: %s", stale.Body.String())
	}
}

func TestWorkspaceModelConfigProjectsSystemChannelsForMembers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	store, err := workspace.NewProviderConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocalModelConfig([]byte(`{"channels":[{"id":"disk-user","scope":"user","baseUrl":"https://user.invalid/v1","apiKey":"disk-user-secret","enabled":true}]}`)); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open("file:member-model-config?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelChannel{}, &model.ChannelModel{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ModelChannel{
		ID: "platform-channel", Name: "内部平台渠道", PublicAlias: "创作精选", Scope: model.ChannelScopeSystem,
		Enabled: true, BaseURL: "https://platform.invalid/v1", APIKey: "platform-secret", APIFormat: "openai",
		HeadersJSON: `[{"name":"Authorization","value":"Bearer platform-header-secret"}]`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ChannelModel{
		ID: "platform-model", ChannelID: "platform-channel", ModelKey: "platform-text", DisplayName: "平台文本",
		Capability: "text", Protocol: model.ChannelInterfaceChatCompletion, Enabled: true,
	}).Error; err != nil {
		t.Fatal(err)
	}

	service := app.New(repository.New(db), dir)
	router := gin.New()
	router.Use(WorkspaceMiddleware(workspace.Context{ID: "u_member", DataDir: dir}))
	router.Use(RuntimeDependenciesMiddleware(RuntimeDependencies{ProviderConfig: store}))
	router.Use(func(c *gin.Context) {
		c.Set(principalContextKey, identity.Principal{UserID: "u_member", Role: identity.RoleUser, DisplayName: "Member", SID: "sid-member"})
		c.Set(identityEnabledContextKey, true)
		c.Next()
	})
	RegisterWorkspaceRoutes(router.Group("/api"), service)

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/workspace/model-config", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", get.Code, get.Body.String())
	}
	config, _ := modelConfigResponseData(t, get)["config"].(map[string]any)
	channels, _ := config["channels"].([]any)
	if len(channels) != 1 {
		t.Fatalf("member channels = %#v", channels)
	}
	channel, _ := channels[0].(map[string]any)
	if channel["id"] != "platform-channel" || channel["scope"] != "system" {
		t.Fatalf("member system channel projection = %#v", channel)
	}
	if channel["apiKey"] != "system" || channel["baseUrl"] != "/api/ai/system/platform-channel" {
		t.Fatalf("member channel credentials were not projected safely: %#v", channel)
	}
	if _, exists := channel["headers"]; exists {
		t.Fatalf("member channel exposed outbound headers: %#v", channel)
	}
	for _, secret := range []string{"disk-user-secret", "platform-secret", "platform-header-secret", "https://platform.invalid/v1"} {
		if bytes.Contains(get.Body.Bytes(), []byte(secret)) {
			t.Fatalf("member response leaked %q: %s", secret, get.Body.String())
		}
	}

	put := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/workspace/model-config", bytes.NewReader([]byte(`{"config":{"channels":[]},"expectedRevision":0}`)))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(put, request)
	if put.Code != http.StatusForbidden {
		t.Fatalf("member put status = %d body=%s", put.Code, put.Body.String())
	}
}

func newModelConfigTestRouter(t *testing.T) (*gin.Engine, *workspace.ProviderConfig) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	store, err := workspace.NewProviderConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := app.NewLocal(nil, dir)
	router := gin.New()
	router.Use(WorkspaceMiddleware(workspace.Context{ID: "local", DataDir: dir}))
	router.Use(RuntimeDependenciesMiddleware(RuntimeDependencies{ProviderConfig: store}))
	RegisterWorkspaceRoutes(router.Group("/api"), service)
	return router, store
}

func putModelConfig(t *testing.T, router *gin.Engine, expectedRevision int64, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"expectedRevision": expectedRevision,
		"config":           map[string]any{"channels": []any{map[string]any{"id": "beefapi", "apiKey": apiKey, "enabled": true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/workspace/model-config", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func modelConfigResponseData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
