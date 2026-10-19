// Package brokeraitools provides bounded, ephemeral tool credentials. Business
// operations remain in the existing community Device and CommandData owners.
package brokeraitools

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	Describe        = "devices.describe"
	Telemetry       = "telemetry.query"
	Preview         = "commands.preview"
	Result          = "commands.getResult"
	Lifetime        = 120 * time.Second
	MaxCalls        = 20
	MaxDevices      = 16
	MaxUserGrants   = 8
	MaxTenantGrants = 1000
	keyPrefix       = "broker:ai:v1:"
)

var (
	ErrInvalid      = errors.New("invalid tool delegation")
	ErrUnauthorized = errors.New("tool delegation unauthorized or expired")
	ErrBudget       = errors.New("tool call budget exceeded")
	ErrCapacity     = errors.New("tool delegation capacity exceeded")
	ErrUnavailable  = errors.New("tool delegation service unavailable")
)

type Subject struct {
	TenantID     string `json:"tenant_id"`
	UserID       string `json:"user_id"`
	Authority    string `json:"authority"`
	SessionToken string `json:"-"`
}

// Credential is private runtime context. JSON encoding never exports the
// original human session; APIs must return the explicit Issued DTO only.
type Credential struct {
	ID        string   `json:"id"`
	Subject   Subject  `json:"subject"`
	DeviceIDs []string `json:"device_ids"`
	Scopes    []string `json:"scopes"`
}

type Issued struct {
	ID        string    `json:"delegation_id"`
	TenantID  string    `json:"tenant_id"`
	SubjectID string    `json:"subject_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	DeviceIDs []string  `json:"device_ids"`
	Scopes    []string  `json:"scopes"`
	MaxCalls  int       `json:"max_calls"`
}

type storedCredential struct {
	Version       int        `json:"version"`
	Credential    Credential `json:"credential"`
	ParentSession string     `json:"parent_session"`
}

type Store struct{ client redis.UniversalClient }

func New(client redis.UniversalClient) *Store { return &Store{client: client} }

func field(s string, n int) bool {
	return s != "" && len(s) <= n && strings.TrimSpace(s) == s && !strings.ContainsFunc(s, unicode.IsControl)
}

func validSubject(s Subject) bool {
	return field(s.TenantID, 36) && field(s.UserID, 36) && field(s.Authority, 32) && field(s.SessionToken, 8192)
}

func validScope(s string) bool { return s == Describe || s == Telemetry || s == Preview || s == Result }

func ValidateScope(devices, scopes []string) error {
	if len(devices) == 0 || len(devices) > MaxDevices || len(scopes) == 0 || len(scopes) > 4 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, id := range devices {
		if !field(id, 36) || strings.ContainsAny(id, "/+#") || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	seen = map[string]bool{}
	for _, op := range scopes {
		if !validScope(op) || seen[op] {
			return ErrInvalid
		}
		seen[op] = true
	}
	return nil
}

func digest(s string) string     { d := sha256.Sum256([]byte(s)); return hex.EncodeToString(d[:]) }
func userKey(s Subject) string   { return keyPrefix + "user:" + digest(s.TenantID+"\x00"+s.UserID) }
func tenantKey(s Subject) string { return keyPrefix + "tenant:" + digest(s.TenantID) }
func idKey(id string) string     { return keyPrefix + "id:" + id }
func tokenKey(token string) (string, error) {
	if len(token) != 48 || !strings.HasPrefix(token, "tpai_") {
		return "", ErrUnauthorized
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "tpai_"))
	if err != nil || len(b) != 32 {
		return "", ErrUnauthorized
	}
	return keyPrefix + "grant:" + digest(token), nil
}

const issueScript = `
local t=redis.call('TIME')
local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now)
redis.call('ZREMRANGEBYSCORE',KEYS[3],'-inf',now)
if redis.call('ZCARD',KEYS[2])>=tonumber(ARGV[3]) or redis.call('ZCARD',KEYS[3])>=tonumber(ARGV[4]) then return -1 end
if redis.call('EXISTS',KEYS[1])~=0 or redis.call('EXISTS',KEYS[4])~=0 then return -2 end
local deadline=now+tonumber(ARGV[2])
redis.call('HSET',KEYS[1],'body',ARGV[1],'remaining',ARGV[5])
redis.call('PEXPIRE',KEYS[1],ARGV[2])
redis.call('SET',KEYS[4],KEYS[1],'PX',ARGV[2])
redis.call('ZADD',KEYS[2],deadline,KEYS[1])
redis.call('ZADD',KEYS[3],deadline,KEYS[1])
redis.call('PEXPIRE',KEYS[2],tonumber(ARGV[2])+1000)
redis.call('PEXPIRE',KEYS[3],tonumber(ARGV[2])+1000)
return deadline`

// Issue must be called only after the human session and all device scopes have
// been verified by the existing business owner. Expiry uses Redis time.
func (s *Store) Issue(ctx context.Context, subject Subject, devices, scopes []string) (Issued, error) {
	if ctx == nil || !validSubject(subject) || ValidateScope(devices, scopes) != nil {
		return Issued{}, ErrInvalid
	}
	if s == nil || s.client == nil {
		return Issued{}, ErrUnavailable
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Issued{}, ErrUnavailable
	}
	token := "tpai_" + base64.RawURLEncoding.EncodeToString(random[:])
	key, _ := tokenKey(token)
	c := Credential{ID: uuid.NewString(), Subject: subject, DeviceIDs: append([]string(nil), devices...), Scopes: append([]string(nil), scopes...)}
	body, err := json.Marshal(storedCredential{Version: 1, Credential: c, ParentSession: subject.SessionToken})
	if err != nil || len(body) > 16<<10 {
		return Issued{}, ErrInvalid
	}
	deadline, err := s.client.Eval(ctx, issueScript, []string{key, userKey(subject), tenantKey(subject), idKey(c.ID)}, string(body), Lifetime.Milliseconds(), MaxUserGrants, MaxTenantGrants, MaxCalls).Int64()
	if err != nil {
		return Issued{}, ErrUnavailable
	}
	if deadline == -1 {
		return Issued{}, ErrCapacity
	}
	if deadline < 0 {
		return Issued{}, ErrUnavailable
	}
	return Issued{ID: c.ID, TenantID: subject.TenantID, SubjectID: subject.UserID, Token: token, ExpiresAt: time.UnixMilli(deadline).UTC(), DeviceIDs: c.DeviceIDs, Scopes: c.Scopes, MaxCalls: MaxCalls}, nil
}

func decode(body string) (Credential, error) {
	if len(body) == 0 || len(body) > 16<<10 {
		return Credential{}, ErrUnavailable
	}
	var record storedCredential
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&record) != nil || record.Version != 1 {
		return Credential{}, ErrUnavailable
	}
	c := record.Credential
	c.Subject.SessionToken = record.ParentSession
	id, err := uuid.Parse(c.ID)
	if err != nil || id.String() != c.ID || !validSubject(c.Subject) || ValidateScope(c.DeviceIDs, c.Scopes) != nil {
		return Credential{}, ErrUnavailable
	}
	return c, nil
}

const consumeScript = `
if redis.call('PTTL',KEYS[1])<=0 or redis.call('HGET',KEYS[1],'body')~=ARGV[1] then return -1 end
local remaining=tonumber(redis.call('HGET',KEYS[1],'remaining'))
if not remaining then return -1 end
if remaining<=0 then return -2 end
redis.call('HINCRBY',KEYS[1],'remaining',-1)
return remaining-1`

// Consume checks the immutable operation/device scope and atomically consumes
// one call before the service revalidates the parent session. No operation has
// permission to authorize or submit a device command.
func (s *Store) Consume(ctx context.Context, token, operation, deviceID string) (Credential, error) {
	if ctx == nil {
		return Credential{}, ErrInvalid
	}
	if s == nil || s.client == nil {
		return Credential{}, ErrUnavailable
	}
	key, err := tokenKey(token)
	if err != nil {
		return Credential{}, err
	}
	body, err := s.client.HGet(ctx, key, "body").Result()
	if errors.Is(err, redis.Nil) {
		return Credential{}, ErrUnauthorized
	}
	if err != nil {
		return Credential{}, ErrUnavailable
	}
	c, err := decode(body)
	if err != nil {
		return Credential{}, err
	}
	allowedOp, allowedDevice := false, false
	for _, op := range c.Scopes {
		if op == operation {
			allowedOp = true
		}
	}
	for _, id := range c.DeviceIDs {
		if id == deviceID {
			allowedDevice = true
		}
	}
	if !allowedOp || !allowedDevice {
		return Credential{}, ErrUnauthorized
	}
	remaining, err := s.client.Eval(ctx, consumeScript, []string{key}, body).Int64()
	if err != nil {
		return Credential{}, ErrUnavailable
	}
	if remaining == -2 {
		return Credential{}, ErrBudget
	}
	if remaining < 0 {
		return Credential{}, ErrUnauthorized
	}
	return c, nil
}

const revokeScript = `
if redis.call('HGET',KEYS[1],'body')~=ARGV[1] then return 0 end
redis.call('DEL',KEYS[1],KEYS[2])
redis.call('ZREM',KEYS[3],KEYS[1])
redis.call('ZREM',KEYS[4],KEYS[1])
return 1`

// Revoke takes a public ID, never a bearer token. The parent identity must have
// been revalidated independently before calling this method.
func (s *Store) Revoke(ctx context.Context, subject Subject, id string) error {
	parsed, err := uuid.Parse(id)
	if ctx == nil || err != nil || parsed.String() != id || !validSubject(subject) {
		return ErrInvalid
	}
	if s == nil || s.client == nil {
		return ErrUnavailable
	}
	key, err := s.client.Get(ctx, idKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return ErrUnauthorized
	}
	if err != nil {
		return ErrUnavailable
	}
	if !strings.HasPrefix(key, keyPrefix+"grant:") || len(key) != len(keyPrefix+"grant:")+64 {
		return ErrUnavailable
	}
	body, err := s.client.HGet(ctx, key, "body").Result()
	if errors.Is(err, redis.Nil) {
		return ErrUnauthorized
	}
	if err != nil {
		return ErrUnavailable
	}
	c, err := decode(body)
	if err != nil {
		return err
	}
	if c.ID != id || c.Subject.UserID != subject.UserID || c.Subject.TenantID != subject.TenantID {
		return ErrUnauthorized
	}
	n, err := s.client.Eval(ctx, revokeScript, []string{key, idKey(id), userKey(c.Subject), tenantKey(c.Subject)}, body).Int()
	if err != nil {
		return ErrUnavailable
	}
	if n != 1 {
		return ErrUnauthorized
	}
	return nil
}
