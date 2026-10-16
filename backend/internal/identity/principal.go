// Package identity owns OrcaTV's OrcaDock-bound request identity.
//
// OrcaDock is the only identity source. OrcaTV never invents a fallback
// principal: every protected request must carry an OrcaTV session that was
// created from a one-time OrcaDock handoff and is still backed by a live
// OrcaDock session at introspection time.
package identity

import (
	"errors"
	"strings"
)

// Principal is the authenticated caller projection used across OrcaTV.
// SID is the opaque HMAC fingerprint of the OrcaDock session token; the raw
// OrcaDock token never reaches OrcaTV.
type Principal struct {
	UserID      string
	Role        string
	DisplayName string
	SID         string
}

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// ErrUnauthenticated is returned whenever a request cannot be tied to a live
// OrcaDock identity. Callers must fail closed on this error.
var ErrUnauthenticated = errors.New("OrcaTV 会话无效")

func (p Principal) Validate() error {
	if strings.TrimSpace(p.UserID) == "" {
		return ErrUnauthenticated
	}
	if strings.TrimSpace(p.SID) == "" {
		return ErrUnauthenticated
	}
	return nil
}

func (p Principal) IsAdmin() bool { return strings.EqualFold(strings.TrimSpace(p.Role), RoleAdmin) }

// NormalizeRole maps an OrcaDock role onto OrcaTV's two known roles. Unknown
// roles degrade to the least-privileged member role.
func NormalizeRole(role string) string {
	if strings.EqualFold(strings.TrimSpace(role), RoleAdmin) {
		return RoleAdmin
	}
	return RoleUser
}
