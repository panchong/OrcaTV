package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubIntrospector struct {
	principal   Principal
	err         error
	requiredSID string
}

func (s stubIntrospector) Introspect(_ context.Context, sid string) (Principal, error) {
	if s.err != nil {
		return Principal{}, s.err
	}
	if s.requiredSID != "" && sid != s.requiredSID {
		return Principal{}, ErrUnauthenticated
	}
	principal := s.principal
	principal.SID = sid
	return principal, nil
}

func newTestService(t *testing.T, intro Introspector) *Service {
	t.Helper()
	return newTestServiceAt(t, t.TempDir(), intro)
}

func newTestServiceAt(t *testing.T, dataDir string, intro Introspector) *Service {
	t.Helper()
	service, err := NewService(Config{
		DataDir:       dataDir,
		HandoffSecret: []byte("test-secret-0123456789abcdef0123456789"),
		SessionTTL:    time.Hour,
		Introspector:  intro,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func validClaims(now time.Time) HandoffClaims {
	return HandoffClaims{
		Issuer: HandoffIssuer, Audience: HandoffAudience, UserID: "u_alice",
		Role: RoleAdmin, DisplayName: "Alice", SID: "sid-alice", JTI: "jti-1",
		ExpiresAt: now.Add(time.Minute),
	}
}

func TestAcceptHandoffHappyPath(t *testing.T) {
	service := newTestService(t, stubIntrospector{})
	now := time.Now().UTC()
	secret := []byte("test-secret-0123456789abcdef0123456789")
	raw, err := SignHandoff(secret, validClaims(now))
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.AcceptHandoff(raw, now)
	if err != nil {
		t.Fatalf("valid handoff rejected: %v", err)
	}
	if claims.UserID != "u_alice" || claims.SID != "sid-alice" || claims.Role != RoleAdmin {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestAcceptHandoffRejectsBadSignature(t *testing.T) {
	service := newTestService(t, stubIntrospector{})
	now := time.Now().UTC()
	raw, err := SignHandoff([]byte("a-completely-different-secret-000000000000"), validClaims(now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptHandoff(raw, now); !errors.Is(err, ErrHandoffRejected) {
		t.Fatalf("bad signature accepted: %v", err)
	}
}

func TestAcceptHandoffRejectsWrongAudience(t *testing.T) {
	service := newTestService(t, stubIntrospector{})
	now := time.Now().UTC()
	claims := validClaims(now)
	claims.Audience = "some-other-service"
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptHandoff(raw, now); !errors.Is(err, ErrHandoffRejected) {
		t.Fatalf("wrong audience accepted: %v", err)
	}
}

func TestAcceptHandoffRejectsWrongIssuer(t *testing.T) {
	service := newTestService(t, stubIntrospector{})
	now := time.Now().UTC()
	claims := validClaims(now)
	claims.Issuer = "some-other-idp"
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptHandoff(raw, now); !errors.Is(err, ErrHandoffRejected) {
		t.Fatalf("wrong issuer accepted: %v", err)
	}
}

func TestAcceptHandoffRejectsExpired(t *testing.T) {
	service := newTestService(t, stubIntrospector{})
	now := time.Now().UTC()
	claims := validClaims(now)
	claims.ExpiresAt = now.Add(-time.Second)
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptHandoff(raw, now); !errors.Is(err, ErrHandoffRejected) {
		t.Fatalf("expired handoff accepted: %v", err)
	}
}

func TestAcceptHandoffRejectsReplayedJTI(t *testing.T) {
	service := newTestService(t, stubIntrospector{})
	now := time.Now().UTC()
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), validClaims(now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptHandoff(raw, now); err != nil {
		t.Fatalf("first handoff rejected: %v", err)
	}
	if _, err := service.AcceptHandoff(raw, now); !errors.Is(err, ErrHandoffRejected) {
		t.Fatalf("replayed jti accepted: %v", err)
	}
}

func TestConsumedHandoffSurvivesServiceRestart(t *testing.T) {
	dataDir := t.TempDir()
	now := time.Now().UTC()
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), validClaims(now))
	if err != nil {
		t.Fatal(err)
	}
	first := newTestServiceAt(t, dataDir, stubIntrospector{})
	if _, err := first.AcceptHandoff(raw, now); err != nil {
		t.Fatalf("first handoff rejected: %v", err)
	}

	restarted := newTestServiceAt(t, dataDir, stubIntrospector{})
	if _, err := restarted.AcceptHandoff(raw, now); !errors.Is(err, ErrHandoffRejected) {
		t.Fatalf("replayed jti accepted after restart: %v", err)
	}
}

func TestAuthenticateFailsClosedWithoutLiveOrcaDockSession(t *testing.T) {
	service := newTestService(t, stubIntrospector{err: ErrUnauthenticated})
	now := time.Now().UTC()
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), validClaims(now))
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
	if _, err := service.Authenticate(context.Background(), cookie); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("authenticate should fail closed: %v", err)
	}
}

func TestAuthenticateRejectsForgedSID(t *testing.T) {
	service := newTestService(t, stubIntrospector{
		principal:   Principal{UserID: "u_alice", Role: RoleAdmin, DisplayName: "Alice"},
		requiredSID: "sid-alice",
	})
	now := time.Now().UTC()
	claims := validClaims(now)
	claims.SID = "forged-sid"
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), claims)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := service.AcceptHandoff(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	cookie, _, err := service.IssueSession(accepted, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), cookie); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("forged sid accepted: %v", err)
	}
}

func TestAuthenticateResolvesLivePrincipal(t *testing.T) {
	service := newTestService(t, stubIntrospector{principal: Principal{UserID: "u_alice", Role: RoleUser, DisplayName: "Alice"}})
	now := time.Now().UTC()
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), validClaims(now))
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
	principal, err := service.Authenticate(context.Background(), cookie)
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}
	if principal.UserID != "u_alice" || principal.SID != "sid-alice" {
		t.Fatalf("principal = %#v", principal)
	}
}

func TestAuthenticateRejectsOwnerSwitch(t *testing.T) {
	service := newTestService(t, stubIntrospector{principal: Principal{UserID: "u_bob", Role: RoleUser, DisplayName: "Bob"}})
	now := time.Now().UTC()
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), validClaims(now))
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
	if _, err := service.Authenticate(context.Background(), cookie); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("owner switch should be rejected: %v", err)
	}
}

func TestRevokeDropsSessions(t *testing.T) {
	service := newTestService(t, stubIntrospector{principal: Principal{UserID: "u_alice", Role: RoleUser, DisplayName: "Alice"}})
	now := time.Now().UTC()
	raw, err := SignHandoff([]byte("test-secret-0123456789abcdef0123456789"), validClaims(now))
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
	if _, err := service.Authenticate(context.Background(), cookie); err != nil {
		t.Fatalf("expected live session: %v", err)
	}
	if revoked := service.RevokeBySID("sid-alice"); revoked != 1 {
		t.Fatalf("RevokeBySID = %d, want 1", revoked)
	}
	if _, err := service.Authenticate(context.Background(), cookie); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked session still authenticated: %v", err)
	}
}

func TestSIDFingerprintIsKeyed(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	first := SIDFingerprint([]byte("key-a"), hash)
	if first != SIDFingerprint([]byte("key-a"), hash) {
		t.Fatal("fingerprint is not deterministic")
	}
	if first == SIDFingerprint([]byte("key-b"), hash) {
		t.Fatal("fingerprint must depend on the secret")
	}
}
