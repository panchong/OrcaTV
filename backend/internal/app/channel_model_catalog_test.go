package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/beefapi"
	"infinite-canvas/backend/internal/model"
)

func TestChannelCatalogErrorsHaveStableReasons(t *testing.T) {
	for _, item := range []struct {
		status int
		reason string
	}{
		{http.StatusUnauthorized, "catalog_auth_failed"},
		{http.StatusForbidden, "catalog_auth_failed"},
		{http.StatusNotFound, "catalog_unsupported"},
		{http.StatusTooManyRequests, "catalog_rate_limited"},
	} {
		err := channelModelsUpstreamError(providerHTTPError{StatusCode: item.status})
		var classified *AppError
		if !errors.As(err, &classified) || string(classified.Reason) != item.reason {
			t.Fatalf("status %d: %#v", item.status, err)
		}
	}
	var timeout *AppError
	if !errors.As(channelModelsUpstreamError(context.DeadlineExceeded), &timeout) || timeout.Reason != "catalog_timeout" {
		t.Fatal("timeout not classified")
	}
}

func TestFetchChannelModelCatalogRejectsCredentialsWithoutNetwork(t *testing.T) {
	svc := &Service{}
	ctx := context.Background()
	actor := &model.User{ID: "user-1", Role: model.UserRoleUser}

	if _, err := svc.FetchChannelModelCatalog(ctx, nil, ChannelModelsRequest{BaseURL: "https://example.com/v1", APIKey: "k"}); err == nil {
		t.Fatal("anonymous catalog fetch accepted")
	}
	if _, err := svc.FetchChannelModelCatalog(ctx, actor, ChannelModelsRequest{BaseURL: "https://example.com/v1"}); err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("empty key error = %v", err)
	}
	if _, err := svc.FetchChannelModelCatalog(ctx, actor, ChannelModelsRequest{
		BaseURL: "https://enterprise.beefapi.com/v1", ChannelID: beefapi.ChannelID, CredentialRef: beefapi.CredentialRef,
	}); err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("missing BeefAPI credential error = %v", err)
	}
	if _, err := svc.FetchChannelModelCatalog(ctx, actor, ChannelModelsRequest{BaseURL: "http://127.0.0.1/v1", APIKey: "k"}); err == nil {
		t.Fatal("loopback catalog fetch accepted")
	} else {
		var auth *AuthError
		if !errors.As(err, &auth) {
			t.Fatalf("loopback error = %#v", err)
		}
	}
}

func TestPlatformChannelModelCatalogDoesNotFallbackToWorkspaceOwner(t *testing.T) {
	svc := &Service{}
	_, err := svc.FetchPlatformChannelModelCatalog(context.Background(), ChannelModelsRequest{BaseURL: "https://example.com/v1"})
	if err == nil {
		t.Fatal("platform catalog accepted an empty credential")
	}
	if strings.Contains(err.Error(), "请先登录") {
		t.Fatalf("platform catalog fell back to user authentication: %v", err)
	}
	if !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("platform catalog did not reach credential validation: %v", err)
	}
}

func TestFetchChannelModelCatalogGeminiLoopbackDoesNotLeaveTheHost(t *testing.T) {
	svc := &Service{}
	_, err := svc.FetchChannelModelCatalog(context.Background(), &model.User{ID: "user-1"}, ChannelModelsRequest{
		BaseURL: "http://127.0.0.1", APIKey: "k", APIFormat: "gemini",
	})
	if err == nil {
		t.Fatal("gemini loopback catalog fetch accepted")
	}
	var auth *AuthError
	if !errors.As(err, &auth) {
		t.Fatalf("gemini loopback error = %#v", err)
	}
}
