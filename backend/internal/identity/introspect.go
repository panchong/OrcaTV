package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Introspector confirms that an OrcaDock session is still live and returns the
// caller's current role and display name. OrcaTV fails closed whenever this
// lookup fails: no OrcaDock, no identity.
type Introspector interface {
	Introspect(ctx context.Context, sid string) (Principal, error)
}

// HTTPIntrospector calls OrcaDock's server-only introspection endpoint using a
// shared service key. The key and endpoint never reach the browser.
type HTTPIntrospector struct {
	endpoint   string
	serviceKey string
	client     *http.Client
}

func NewHTTPIntrospector(endpoint, serviceKey string, timeout time.Duration) (*HTTPIntrospector, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("OrcaTV 未配置 OrcaDock introspect 地址")
	}
	if strings.TrimSpace(serviceKey) == "" {
		return nil, errors.New("OrcaTV 未配置 OrcaDock 服务密钥")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &HTTPIntrospector{
		endpoint:   endpoint,
		serviceKey: serviceKey,
		client:     &http.Client{Timeout: timeout},
	}, nil
}

type introspectResponse struct {
	UserID      string `json:"userId"`
	Role        string `json:"role"`
	DisplayName string `json:"displayName"`
}

func (i *HTTPIntrospector) Introspect(ctx context.Context, sid string) (Principal, error) {
	if i == nil {
		return Principal{}, ErrUnauthenticated
	}
	sid = strings.TrimSpace(sid)
	if sid == "" {
		return Principal{}, ErrUnauthenticated
	}
	body, err := json.Marshal(map[string]string{"sid": sid})
	if err != nil {
		return Principal{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, i.endpoint, bytes.NewReader(body))
	if err != nil {
		return Principal{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+i.serviceKey)
	response, err := i.client.Do(request)
	if err != nil {
		return Principal{}, fmt.Errorf("OrcaDock 会话校验失败: %w", ErrUnauthenticated)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return Principal{}, ErrUnauthenticated
	}
	var payload introspectResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&payload); err != nil {
		return Principal{}, ErrUnauthenticated
	}
	principal := Principal{
		UserID:      strings.TrimSpace(payload.UserID),
		Role:        NormalizeRole(payload.Role),
		DisplayName: strings.TrimSpace(payload.DisplayName),
		SID:         sid,
	}
	if err := principal.Validate(); err != nil {
		return Principal{}, err
	}
	return principal, nil
}
