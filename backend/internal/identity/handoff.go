package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	HandoffIssuer   = "orcadock"
	HandoffAudience = "orcatv"
	handoffAlg      = "HS256"
)

// ErrHandoffRejected covers every handoff failure mode. The concrete reason is
// deliberately not surfaced to callers so a probing client cannot tell a bad
// signature from an expired or replayed token.
var ErrHandoffRejected = errors.New("OrcaTV 握手令牌无效")

// HandoffClaims is the verified payload of an OrcaDock -> OrcaTV handoff JWT.
type HandoffClaims struct {
	Issuer      string
	Audience    string
	UserID      string
	Role        string
	DisplayName string
	SID         string
	JTI         string
	ExpiresAt   time.Time
}

type handoffPayload struct {
	Iss         string `json:"iss"`
	Aud         string `json:"aud"`
	Sub         string `json:"sub"`
	Role        string `json:"role"`
	DisplayName string `json:"displayName"`
	SID         string `json:"sid"`
	JTI         string `json:"jti"`
	Exp         int64  `json:"exp"`
}

// VerifyHandoff validates an HS256 handoff JWT against the shared secret and
// returns the claims. `now` is injected so tests can exercise expiry without
// sleeping.
func VerifyHandoff(secret []byte, raw string, now time.Time) (HandoffClaims, error) {
	if len(secret) == 0 {
		return HandoffClaims{}, fmt.Errorf("OrcaTV 握手密钥未配置: %w", ErrHandoffRejected)
	}
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 3 {
		return HandoffClaims{}, ErrHandoffRejected
	}
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return HandoffClaims{}, ErrHandoffRejected
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	if !hmac.Equal(mac.Sum(nil), signature) {
		return HandoffClaims{}, ErrHandoffRejected
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return HandoffClaims{}, ErrHandoffRejected
	}
	if !strings.EqualFold(header.Alg, handoffAlg) {
		return HandoffClaims{}, ErrHandoffRejected
	}
	var payload handoffPayload
	if err := decodeSegment(parts[1], &payload); err != nil {
		return HandoffClaims{}, ErrHandoffRejected
	}
	claims := HandoffClaims{
		Issuer: strings.TrimSpace(payload.Iss), Audience: strings.TrimSpace(payload.Aud),
		UserID: strings.TrimSpace(payload.Sub), Role: NormalizeRole(payload.Role),
		DisplayName: strings.TrimSpace(payload.DisplayName), SID: strings.TrimSpace(payload.SID),
		JTI: strings.TrimSpace(payload.JTI),
	}
	if payload.Exp <= 0 {
		return HandoffClaims{}, ErrHandoffRejected
	}
	claims.ExpiresAt = time.Unix(payload.Exp, 0).UTC()
	if claims.Issuer != HandoffIssuer || claims.Audience != HandoffAudience {
		return HandoffClaims{}, ErrHandoffRejected
	}
	if claims.UserID == "" || claims.SID == "" || claims.JTI == "" {
		return HandoffClaims{}, ErrHandoffRejected
	}
	if !now.UTC().Before(claims.ExpiresAt) {
		return HandoffClaims{}, ErrHandoffRejected
	}
	return claims, nil
}

// SignHandoff is the mirror used by tests and fixtures to mint a handoff token.
func SignHandoff(secret []byte, claims HandoffClaims) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("OrcaTV 握手密钥未配置")
	}
	payload, err := json.Marshal(handoffPayload{
		Iss: claims.Issuer, Aud: claims.Audience, Sub: claims.UserID, Role: claims.Role,
		DisplayName: claims.DisplayName, SID: claims.SID, JTI: claims.JTI,
		Exp: claims.ExpiresAt.UTC().Unix(),
	})
	if err != nil {
		return "", err
	}
	header, err := json.Marshal(map[string]string{"alg": handoffAlg, "typ": "JWT"})
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodeSegment(segment string, target any) error {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

// SIDFingerprint derives the opaque OrcaDock session fingerprint:
// hex(HMAC-SHA256(secret, lowerHex(sha256(token)))). OrcaDock stores only the
// SHA-256 of its session token, so OrcaTV never needs the raw token.
func SIDFingerprint(secret []byte, tokenSHA256Hex string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strings.ToLower(strings.TrimSpace(tokenSHA256Hex))))
	return hex.EncodeToString(mac.Sum(nil))
}
