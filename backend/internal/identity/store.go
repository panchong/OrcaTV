package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// handoffRetention bounds how long a consumed jti is remembered. Handoffs live
// for 60s; anything older than this can never be replayed successfully anyway.
const handoffRetention = 10 * time.Minute

// SessionRecord is the durable server-side half of an OrcaTV session. The
// browser only ever holds the opaque cookie; everything else lives here.
type SessionRecord struct {
	UserID      string    `json:"userId"`
	SID         string    `json:"sid"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type storeData struct {
	Handoffs map[string]time.Time    `json:"handoffs"`
	Sessions map[string]SessionRecord `json:"sessions"`
}

// Store persists consumed handoff ids and issued sessions. It is a small
// single-process JSON store: sessions are low-volume and must survive restarts
// so a handoff jti cannot be replayed after a crash.
type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("OrcaTV 身份存储路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return &Store{path: path}, nil
}

// ConsumeHandoff records a jti as used. It returns an error when the jti was
// already consumed, which is how replay attempts are rejected.
func (s *Store) ConsumeHandoff(jti string, now time.Time) error {
	if jti == "" {
		return ErrHandoffRejected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadLocked()
	if err != nil {
		return err
	}
	if _, used := data.Handoffs[jti]; used {
		return fmt.Errorf("握手令牌已使用: %w", ErrHandoffRejected)
	}
	data.Handoffs[jti] = now.UTC()
	return s.saveLocked(data, now)
}

func (s *Store) PutSession(tokenHash string, record SessionRecord) error {
	if tokenHash == "" {
		return errors.New("OrcaTV 会话标识不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadLocked()
	if err != nil {
		return err
	}
	data.Sessions[tokenHash] = record
	return s.saveLocked(data, time.Now())
}

// Session looks up a live session by cookie token hash. Expired or unknown
// sessions report ok=false; callers must fail closed.
func (s *Store) Session(tokenHash string) (SessionRecord, bool) {
	if tokenHash == "" {
		return SessionRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadLocked()
	if err != nil {
		return SessionRecord{}, false
	}
	record, ok := data.Sessions[tokenHash]
	if !ok {
		return SessionRecord{}, false
	}
	if !time.Now().UTC().Before(record.ExpiresAt) {
		return SessionRecord{}, false
	}
	return record, true
}

// RevokeBySID drops every local session bound to an OrcaDock session id.
func (s *Store) RevokeBySID(sid string) int {
	if sid == "" {
		return 0
	}
	return s.revoke(func(record SessionRecord) bool { return record.SID == sid })
}

// RevokeByUser drops every local session for a user. Used when OrcaDock deletes
// a user or rotates their credentials.
func (s *Store) RevokeByUser(userID string) int {
	if userID == "" {
		return 0
	}
	return s.revoke(func(record SessionRecord) bool { return record.UserID == userID })
}

func (s *Store) revoke(match func(SessionRecord) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadLocked()
	if err != nil {
		return 0
	}
	removed := 0
	for hash, record := range data.Sessions {
		if match(record) {
			delete(data.Sessions, hash)
			removed++
		}
	}
	if removed > 0 {
		_ = s.saveLocked(data, time.Now())
	}
	return removed
}

func (s *Store) loadLocked() (storeData, error) {
	data := storeData{Handoffs: map[string]time.Time{}, Sessions: map[string]SessionRecord{}}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return data, nil
		}
		return data, err
	}
	if len(raw) == 0 {
		return data, nil
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return storeData{}, fmt.Errorf("解析 OrcaTV 身份存储失败: %w", err)
	}
	if data.Handoffs == nil {
		data.Handoffs = map[string]time.Time{}
	}
	if data.Sessions == nil {
		data.Sessions = map[string]SessionRecord{}
	}
	return data, nil
}

func (s *Store) saveLocked(data storeData, now time.Time) error {
	pruneStore(&data, now)
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return atomicWrite(s.path, encoded)
}

func pruneStore(data *storeData, now time.Time) {
	for jti, consumedAt := range data.Handoffs {
		if now.UTC().Sub(consumedAt) > handoffRetention {
			delete(data.Handoffs, jti)
		}
	}
	for hash, record := range data.Sessions {
		if !now.UTC().Before(record.ExpiresAt) {
			delete(data.Sessions, hash)
		}
	}
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".orcatv-identity-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
