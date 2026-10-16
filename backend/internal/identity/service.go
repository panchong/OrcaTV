package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const (
	// CookieName is the OrcaTV session cookie. It is HttpOnly and, in
	// production HTTPS deployments, Secure.
	CookieName = "orcatv-session"
	// DefaultSessionTTL is the ceiling on a local OrcaTV session. Every request
	// still re-checks OrcaDock, so this only bounds how long a browser may reuse
	// its cookie.
	DefaultSessionTTL = 24 * time.Hour
	identityStoreFile = "orcatv-identity.json"
)

// Config wires the identity service to its OrcaDock counterpart.
type Config struct {
	DataDir       string
	HandoffSecret []byte
	SessionTTL    time.Duration
	Secure        bool
	Introspector  Introspector
}

// Service is OrcaTV's identity boundary: it verifies OrcaDock handoffs, issues
// and validates OrcaTV sessions, and re-checks the live OrcaDock session on
// every authenticated request.
type Service struct {
	store        *Store
	secret       []byte
	sessionTTL   time.Duration
	secure       bool
	introspector Introspector
}

func NewService(cfg Config) (*Service, error) {
	if len(cfg.HandoffSecret) == 0 {
		return nil, errors.New("OrcaTV 握手密钥不能为空")
	}
	if cfg.Introspector == nil {
		return nil, errors.New("OrcaTV 缺少 OrcaDock 会话校验客户端")
	}
	ttl := cfg.SessionTTL
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	store, err := NewStore(filepath.Join(cfg.DataDir, identityStoreFile))
	if err != nil {
		return nil, err
	}
	return &Service{
		store: store, secret: cfg.HandoffSecret, sessionTTL: ttl,
		secure: cfg.Secure, introspector: cfg.Introspector,
	}, nil
}

func (s *Service) SessionTTL() time.Duration {
	if s == nil || s.sessionTTL <= 0 {
		return DefaultSessionTTL
	}
	return s.sessionTTL
}

func (s *Service) Secure() bool { return s != nil && s.secure }

// AcceptHandoff verifies a handoff JWT and consumes its jti exactly once. A
// replayed jti, bad signature, wrong audience, or expired token all fail.
func (s *Service) AcceptHandoff(raw string, now time.Time) (HandoffClaims, error) {
	if s == nil {
		return HandoffClaims{}, ErrHandoffRejected
	}
	claims, err := VerifyHandoff(s.secret, raw, now)
	if err != nil {
		return HandoffClaims{}, err
	}
	if err := s.store.ConsumeHandoff(claims.JTI, now); err != nil {
		return HandoffClaims{}, err
	}
	return claims, nil
}

// IssueSession mints a local session cookie value bound to the handoff claims.
func (s *Service) IssueSession(claims HandoffClaims, now time.Time) (string, SessionRecord, error) {
	if s == nil {
		return "", SessionRecord{}, ErrUnauthenticated
	}
	raw, err := randomToken()
	if err != nil {
		return "", SessionRecord{}, err
	}
	record := SessionRecord{
		UserID: claims.UserID, SID: claims.SID, DisplayName: claims.DisplayName,
		Role: NormalizeRole(claims.Role), CreatedAt: now.UTC(), ExpiresAt: now.UTC().Add(s.SessionTTL()),
	}
	if err := s.store.PutSession(hashToken(raw), record); err != nil {
		return "", SessionRecord{}, err
	}
	return raw, record, nil
}

// Authenticate resolves a cookie value to a live Principal. It requires both a
// locally issued, unexpired session and a successful OrcaDock introspection of
// the bound OrcaDock session id.
func (s *Service) Authenticate(ctx context.Context, cookieValue string) (Principal, error) {
	if s == nil {
		return Principal{}, ErrUnauthenticated
	}
	if ctx == nil {
		ctx = context.Background()
	}
	record, ok := s.store.Session(hashToken(cookieValue))
	if !ok {
		return Principal{}, ErrUnauthenticated
	}
	principal, err := s.introspector.Introspect(ctx, record.SID)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	if !strings.EqualFold(principal.UserID, record.UserID) {
		// The OrcaDock session changed owners; refuse rather than trust either side.
		return Principal{}, ErrUnauthenticated
	}
	principal.SID = record.SID
	if err := principal.Validate(); err != nil {
		return Principal{}, err
	}
	return principal, nil
}

func (s *Service) RevokeBySID(sid string) int {
	if s == nil {
		return 0
	}
	return s.store.RevokeBySID(sid)
}

func (s *Service) RevokeByUser(userID string) int {
	if s == nil {
		return 0
	}
	return s.store.RevokeByUser(userID)
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
