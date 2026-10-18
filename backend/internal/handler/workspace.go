package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"infinite-canvas/backend/internal/app"
	"infinite-canvas/backend/internal/beefapi"
	"infinite-canvas/backend/internal/workspace"

	"github.com/gin-gonic/gin"
)

// RegisterWorkspaceRoutes exposes the desktop-first bootstrap contract. The
// workspace scope is injected by the runtime before the router is mounted, so
// this endpoint never reads a cookie or creates an AuthSession.
func RegisterWorkspaceRoutes(r *gin.RouterGroup, svc *app.Service) {
	r.GET("/workspace/bootstrap", func(c *gin.Context) {
		scope, err := CurrentWorkspace(c)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		user, err := currentUser(c, svc)
		if err != nil {
			fail(c, http.StatusUnauthorized, err)
			return
		}
		publicUser := app.AuthUser{User: *user}
		// Desktop model configuration is persisted separately under the workspace
		// data directory. The bootstrap contract contains no account metadata.
		logicalModels := []app.PublicLogicalModel{}
		limits, err := svc.PublicRuntimeLimits()
		if err != nil {
			failService(c, err)
			return
		}
		features, err := svc.FeatureAvailability()
		if err != nil {
			failService(c, err)
			return
		}
		contract := LocalDesktopContract()
		storageMode := "local"
		if IsOrcaTVIdentityEnabled(c) {
			contract = HostedDesktopContract()
			storageMode = "remote"
		}
		ok(c, gin.H{
			"contractVersion": contract.Version,
			"profile":         contract.Profile,
			"capabilities":    contract.Capabilities,
			"workspace": gin.H{
				"id":      scope.ID,
				"name":    user.DisplayName,
				"owner":   user.Username,
				"storage": "sqlite",
			},
			"user":          publicUser,
			"storageMode":   storageMode,
			"logicalModels": logicalModels,
			"runtimeLimits": limits,
			"features":      features,
		})
	})
	r.GET("/workspace/model-config", func(c *gin.Context) {
		if _, err := workspaceForLocalRequest(c, svc); err != nil {
			fail(c, http.StatusUnauthorized, err)
			return
		}
		providerConfig := requestProviderConfig(c, svc)
		if versioned, supportsVersioning := providerConfig.(VersionedProviderConfig); supportsVersioning {
			effective, health, err := versioned.LoadEffectiveModelConfig()
			if err != nil {
				failService(c, err)
				return
			}
			ok(c, gin.H{"config": redactModelConfig(c, svc, effective.Config), "revision": effective.Revision, "health": health, "source": "builtin+local"})
			return
		}
		body, err := providerConfig.ReadLocalModelConfig()
		if err != nil {
			failService(c, err)
			return
		}
		if len(body) == 0 {
			ok(c, gin.H{"config": nil})
			return
		}
		var config map[string]any
		if err := json.Unmarshal(body, &config); err != nil {
			fail(c, http.StatusInternalServerError, errors.New("本地模型配置损坏"))
			return
		}
		ok(c, gin.H{"config": redactModelConfig(c, svc, config)})
	})
	r.PUT("/workspace/model-config", func(c *gin.Context) {
		if _, err := workspaceForLocalRequest(c, svc); err != nil {
			fail(c, http.StatusUnauthorized, err)
			return
		}
		// Model channels are platform-level configuration. Only an authenticated
		// admin may change them; members read a redacted projection elsewhere.
		if _, err := CurrentPrincipal(c); err == nil {
			if _, ok := RequireAdmin(c); !ok {
				return
			}
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20))
		if err != nil {
			fail(c, http.StatusBadRequest, errors.New("本地模型配置读取失败"))
			return
		}
		var envelope struct {
			Config           json.RawMessage `json:"config"`
			ExpectedRevision *int64          `json:"expectedRevision"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Config) == 0 || string(envelope.Config) == "null" {
			fail(c, http.StatusBadRequest, errors.New("本地模型配置格式错误"))
			return
		}
		if _, external := externalBusiness(c); external && envelope.ExpectedRevision == nil {
			fail(c, http.StatusBadRequest, errors.New("修改模型配置需要 expectedRevision"))
			return
		}
		envelope.Config = preserveManagedModelConfig(c, svc, envelope.Config)
		providerConfig := requestProviderConfig(c, svc)
		if versioned, supportsVersioning := providerConfig.(VersionedProviderConfig); supportsVersioning && envelope.ExpectedRevision != nil {
			revision, err := versioned.SaveLocalModelConfigRevision(envelope.Config, *envelope.ExpectedRevision)
			if errors.Is(err, workspace.ErrProviderConfigRevisionConflict) {
				fail(c, http.StatusConflict, err)
				return
			}
			if err != nil {
				failService(c, err)
				return
			}
			// Project the saved platform configuration into system channels so
			// members immediately see and can generate with the updated catalog.
			if err := syncPlatformModelConfig(c, svc); err != nil {
				failService(c, err)
				return
			}
			ok(c, gin.H{"saved": true, "revision": revision})
			return
		}
		if err := providerConfig.SaveLocalModelConfig(envelope.Config); err != nil {
			failService(c, err)
			return
		}
		if err := syncPlatformModelConfig(c, svc); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"saved": true})
	})
}

func syncPlatformModelConfig(c *gin.Context, svc *app.Service) error {
	if svc == nil || svc.IsLocalMode() {
		return nil
	}
	providerConfig := requestProviderConfig(c, svc)
	if store, ok := providerConfig.(app.PlatformModelConfigStore); ok {
		return svc.SyncPlatformModelConfig(store)
	}
	return nil
}

func redactModelConfig(c *gin.Context, svc *app.Service, config map[string]any) map[string]any {
	if _, external := externalBusiness(c); external {
		config = redactExternalConfig(config)
	}
	// Non-admin members only ever see sanitized platform channels and model
	// descriptions; provider credentials stay server-side.
	if principal, err := CurrentPrincipal(c); err == nil && !principal.IsAdmin() {
		config = redactExternalConfig(config)
		config = projectPublicSystemChannels(svc, config)
	}
	managed := false
	if connection, err := requestBeefAPI(c, svc); err == nil && connection != nil {
		managed = connection.HasManagedCredential()
		return connection.RedactConfig(config)
	}
	return beefapi.RedactConfig(config, managed)
}

// projectPublicSystemChannels replaces the on-disk platform snapshot with the
// system-scope read model for non-admin members. Members must receive
// scope="system" plus a stable channel id so generation requests carry the
// channel selector the admission layer requires; upstream base URLs and
// credentials are never part of this projection.
func projectPublicSystemChannels(svc *app.Service, config map[string]any) map[string]any {
	if svc == nil || config == nil {
		return config
	}
	channels, err := svc.PublicSystemChannels()
	if err != nil || len(channels) == 0 {
		return config
	}
	encoded, err := json.Marshal(channels)
	if err != nil {
		return config
	}
	var publicChannels []any
	if err := json.Unmarshal(encoded, &publicChannels); err != nil {
		return config
	}
	projected := make(map[string]any, len(config))
	for key, value := range config {
		projected[key] = value
	}
	projected["channels"] = publicChannels
	return projected
}

func preserveManagedModelConfig(c *gin.Context, svc *app.Service, raw json.RawMessage) json.RawMessage {
	var incoming map[string]any
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return raw
	}
	existing := map[string]any{}
	providerConfig := requestProviderConfig(c, svc)
	if versioned, ok := providerConfig.(VersionedProviderConfig); ok {
		if effective, _, err := versioned.LoadEffectiveModelConfig(); err == nil {
			existing = effective.Config
		}
	}
	managed := false
	if _, external := externalBusiness(c); external {
		preserveExternalConfigSecrets(incoming, existing)
	}
	if connection, err := requestBeefAPI(c, svc); err == nil && connection != nil {
		managed = connection.HasManagedCredential()
		connection.PreserveWrite(incoming, existing)
	} else {
		beefapi.PreserveManagedChannel(incoming, existing, managed)
	}
	encoded, err := json.Marshal(incoming)
	if err != nil {
		return raw
	}
	return encoded
}

func workspaceForLocalRequest(c *gin.Context, svc *app.Service) (workspace.Context, error) {
	if scope, err := CurrentWorkspace(c); err == nil {
		return scope, nil
	}
	if svc.IsLocalMode() {
		return workspace.Context{ID: "local"}, nil
	}
	return workspace.Context{}, errors.New("本地工作区未初始化")
}
