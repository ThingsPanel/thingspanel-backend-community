// Package authcore owns human authentication. It never calls the legacy HTTP
// login/session endpoints and never accepts a machine credential as a person.
package authcore

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUnauthorized = errors.New("invalid or expired credentials")
	ErrUnavailable  = errors.New("authentication dependency unavailable")
	ErrForbidden    = errors.New("operation not permitted")
	ErrConflict     = errors.New("identity already bound or disabled")
	ErrRateLimit    = errors.New("too many attempts")
)

type User struct {
	ActorVersion int64  `json:"-"`
	Delegated    bool   `json:"-"`
	ActorID      string `json:"-"`
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	TenantID     string `json:"tenant_id"`
	Authority    string `json:"authority"`
	Status       string `json:"status"`
	Password     string `json:"-"`
	Version      int64  `json:"-"`
}
type Identity struct{ Issuer, Subject, Provider, ClientID string }
type Binding struct {
	SelfUnlinked bool      `json:"self_unlinked"`
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}
type Accounts interface {
	User(context.Context, string, bool) (User, error)
	Flag(context.Context, string) (bool, error)
	FindIdentity(context.Context, Identity) (User, error)
	Bind(context.Context, User, Identity) error
	Disable(context.Context, User, string) error
	Bindings(context.Context, User) ([]Binding, error)
}
type Claims struct {
	ActorVersion int64  `json:"actor_version,omitempty"`
	Delegated    bool   `json:"delegated,omitempty"`
	ActorID      string `json:"actor_id,omitempty"`
	ID           string `json:"id"`
	Email        string `json:"email"`
	Authority    string `json:"authority"`
	TenantID     string `json:"tenant_id"`
	Version      int64  `json:"auth_version"`
	jwt.RegisteredClaims
}
type Session struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
}
type Service struct {
	Accounts     Accounts
	Redis        redis.UniversalClient
	Key          []byte
	TTL          time.Duration
	MaxFailures  int
	LockDuration time.Duration
	PrivateKey   *rsa.PrivateKey
}

func Role(authority string) (string, error) {
	switch authority {
	case "SYS_ADMIN":
		return "platform_admin", nil
	case "TENANT_ADMIN":
		return "tenant_admin", nil
	case "TENANT_USER":
		return "tenant_viewer", nil
	}
	return "", ErrUnauthorized
}
func admissible(u User) error {
	if u.ID == "" || u.Status != "N" || (u.TenantID == "" && u.Authority != "SYS_ADMIN") {
		return ErrUnauthorized
	}
	_, err := Role(u.Authority)
	return err
}
func Digest(raw string) string  { sum := sha256.Sum256([]byte(raw)); return hex.EncodeToString(sum[:]) }
func PushKey(raw string) string { return "app-push-session:" + Digest(raw) }
func Random() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Service) VerifyPassword(ctx context.Context, login, password, salt string) (User, error) {
	if len(login) > 254 || login == "" || len(password) > 4096 || password == "" || len(salt) > 128 {
		return User{}, ErrUnauthorized
	}
	u, err := s.Accounts.User(ctx, login, true)
	if err != nil && !errors.Is(err, ErrUnauthorized) {
		return User{}, err
	}
	// The same canonical account key is used for email and phone aliases.
	key := Digest(strings.ToLower(login))
	if u.ID != "" {
		key = Digest(u.Email)
	}
	locked, e := s.Redis.Exists(ctx, "auth:lock:"+key, "user:"+u.Email+":lock_until").Result()
	if e != nil {
		return User{}, ErrUnavailable
	}
	if locked > 0 {
		return User{}, ErrRateLimit
	}
	password, e = s.decodePassword(ctx, password, salt)
	if e != nil {
		return User{}, e
	}
	hash := u.Password
	if hash == "" {
		hash = "$2a$10$7EqJtq98hPqEX7fNZaFWoO5qVVNYIo8sV0lT1x.QmQ1K4GdY0bAgu"
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || err != nil || admissible(u) != nil {
		if s.MaxFailures > 0 {
			_, e = s.Redis.Eval(ctx, `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[2]) end; if n>=tonumber(ARGV[1]) then redis.call('SET',KEYS[2],'1','PX',ARGV[2]);redis.call('DEL',KEYS[1]) end;return n`, []string{"auth:fail:" + key, "auth:lock:" + key}, s.MaxFailures, s.LockDuration.Milliseconds()).Result()
			if e != nil {
				return User{}, ErrUnavailable
			}
		}
		return User{}, ErrUnauthorized
	}
	if e = s.Redis.Del(ctx, "auth:fail:"+key, "user:"+u.Email+":failed_attempts").Err(); e != nil {
		return User{}, ErrUnavailable
	}
	return u, nil
}

func (s *Service) Issue(ctx context.Context, u User) (Session, error) { return s.issue(ctx, u, "") }
func (s *Service) Refresh(ctx context.Context, raw string) (Session, error) {
	u, e := s.Check(ctx, raw)
	if e != nil {
		return Session{}, e
	}
	return s.issue(ctx, u, raw)
}
func (s *Service) issue(ctx context.Context, u User, source string) (Session, error) {
	// Re-read account immediately before issuance; stale proof cannot issue a new version.
	current, e := s.Accounts.User(ctx, u.ID, false)
	if e != nil {
		return Session{}, e
	}
	if admissible(current) != nil || current.Version != u.Version {
		return Session{}, ErrUnauthorized
	}
	if len(s.Key) < 32 || s.TTL <= 0 || s.TTL > 30*24*time.Hour {
		return Session{}, ErrUnavailable
	}
	share, e := s.Accounts.Flag(ctx, "shared_account")
	if e != nil {
		return Session{}, e
	}
	id, e := Random()
	if e != nil {
		return Session{}, ErrUnavailable
	}
	now := time.Now()
	c := Claims{ActorVersion: u.ActorVersion, Delegated: u.Delegated, ActorID: u.ActorID, ID: current.ID, Email: current.Email, Authority: current.Authority, TenantID: current.TenantID, Version: current.Version, RegisteredClaims: jwt.RegisteredClaims{ID: id, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.TTL))}}
	raw, e := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.Key)
	if e != nil {
		return Session{}, ErrUnavailable
	}
	// Compare-and-swap also removes a pre-migration session's derived push key.
	// Reading the old token outside Lua is safe only with this equality check.
	for attempt := 0; attempt < 8; attempt++ {
		old := ""
		if !share {
			old, e = s.Redis.Get(ctx, current.Email+"_token").Result()
			if e != nil && !errors.Is(e, redis.Nil) {
				return Session{}, ErrUnavailable
			}
		}
		n, err := s.Redis.Eval(ctx, `if ARGV[5]~='' and redis.call('GET',KEYS[5])~='1' then return -1 end; if ARGV[3]=='0' then
          local old=redis.call('GET',KEYS[2]) or '';
          if old~=ARGV[4] then return 0 end;
          if old~='' then redis.call('DEL',old,KEYS[4]); end;
          local push=redis.call('GET',KEYS[3]);if push then redis.call('DEL',push) end;
          redis.call('SET',KEYS[2],KEYS[1],'PX',ARGV[1]);
          redis.call('SET',KEYS[3],ARGV[2],'PX',ARGV[1]);
        end;redis.call('SET',KEYS[1],'1','PX',ARGV[1]);if ARGV[5]~='' then redis.call('DEL',KEYS[5],KEYS[6]) end;return 1`,
			[]string{raw, current.Email + "_token", "auth:single-push:" + current.ID, PushKey(old), source, PushKey(source)},
			s.TTL.Milliseconds(), PushKey(raw), boolDigit(share), old, source).Int()
		if err != nil {
			return Session{}, ErrUnavailable
		}
		if n == -1 {
			return Session{}, ErrUnauthorized
		}
		if n == 1 {
			return Session{Token: raw, ExpiresIn: int64(s.TTL.Seconds())}, nil
		}
	}
	return Session{}, ErrUnavailable
}
func boolDigit(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
func (s *Service) Check(ctx context.Context, raw string) (User, error) {
	if raw == "" || len(raw) > 8192 {
		return User{}, ErrUnauthorized
	}
	var c Claims
	t, e := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return s.Key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if e != nil || !t.Valid || c.ID == "" || c.Version < 0 || (c.Delegated && c.ActorID == "") {
		return User{}, ErrUnauthorized
	}
	u, e := s.Accounts.User(ctx, c.ID, false)
	if e != nil {
		return User{}, e
	}
	if admissible(u) != nil || u.Version != c.Version || u.TenantID != c.TenantID || u.Authority != c.Authority {
		return User{}, ErrUnauthorized
	}
	if c.Delegated {
		actor, e := s.Accounts.User(ctx, c.ActorID, false)
		if e != nil {
			return User{}, e
		}
		if admissible(actor) != nil || actor.Version != c.ActorVersion || (actor.Authority != "SYS_ADMIN" && !(actor.Authority == "TENANT_ADMIN" && actor.TenantID == u.TenantID && u.Authority != "SYS_ADMIN")) {
			return User{}, ErrUnauthorized
		}
	}
	ttl := time.Until(c.ExpiresAt.Time)
	if s.TTL < ttl {
		ttl = s.TTL
	}
	if ttl <= 0 {
		return User{}, ErrUnauthorized
	}
	n, e := s.Redis.Eval(ctx, `if redis.call('GET',KEYS[1])~='1' then return 0 end;redis.call('PEXPIRE',KEYS[1],ARGV[1]);redis.call('PEXPIRE',KEYS[2],ARGV[1]);return 1`, []string{raw, PushKey(raw)}, ttl.Milliseconds()).Int()
	if e != nil {
		return User{}, ErrUnavailable
	}
	if n != 1 {
		return User{}, ErrUnauthorized
	}
	u.ActorVersion = c.ActorVersion
	u.Delegated = c.Delegated
	u.ActorID = c.ActorID
	return u, nil
}
func (s *Service) Logout(ctx context.Context, raw string) error {
	// Redis session keys are raw JWTs; never let an unauthenticated string
	// delete an arbitrary Redis key. Repeated logout remains idempotent.
	var claims Claims
	if len(raw) == 0 || len(raw) > 8192 {
		return ErrUnauthorized
	}
	token, e := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return s.Key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if e != nil || !token.Valid || claims.ID == "" {
		return ErrUnauthorized
	}

	if e := s.Redis.Del(ctx, raw, PushKey(raw)).Err(); e != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Service) Bind(ctx context.Context, raw, password, salt string, id Identity) error {
	u, e := s.Check(ctx, raw)
	if e != nil {
		return e
	}
	if u.Delegated {
		return ErrForbidden
	}
	proof, e := s.VerifyPassword(ctx, u.Email, password, salt)
	if e != nil {
		return e
	}
	if proof.ID != u.ID || proof.Version != u.Version {
		return ErrUnauthorized
	}
	if _, e = s.Check(ctx, raw); e != nil {
		return e
	}
	return s.Accounts.Bind(ctx, u, id)
}

// Claim is global across login and bind purposes for the same app and code.
func (s *Service) Claim(ctx context.Context, namespace, secret string, ttl time.Duration) error {
	ok, e := s.Redis.SetNX(ctx, "auth:used:"+Digest(namespace+"\x00"+secret), "1", ttl).Result()
	if e != nil {
		return ErrUnavailable
	}
	if !ok {
		return ErrUnauthorized
	}
	return nil
}
func (s *Service) Put(ctx context.Context, kind, secret string, value any, ttl time.Duration) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	if e = s.Redis.Set(ctx, "auth:"+kind+":"+Digest(secret), b, ttl).Err(); e != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Service) Take(ctx context.Context, kind, secret string, value any) error {
	if len(secret) < 32 || len(secret) > 256 {
		return ErrUnauthorized
	}
	b, e := s.Redis.GetDel(ctx, "auth:"+kind+":"+Digest(secret)).Bytes()
	if errors.Is(e, redis.Nil) {
		return ErrUnauthorized
	}
	if e != nil {
		return ErrUnavailable
	}
	if json.Unmarshal(b, value) != nil {
		return ErrUnauthorized
	}
	return nil
}
func (s *Service) Limit(ctx context.Context, key string, maximum int) error {
	n, e := s.Redis.Eval(ctx, `local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],60) end;return n`, []string{"auth:rate:" + Digest(key)}).Int()
	if e != nil {
		return ErrUnavailable
	}
	if n > maximum {
		return ErrRateLimit
	}
	return nil
}
func IdentityValid(i Identity) error {
	if i.Issuer == "" || i.Subject == "" || i.Provider == "" || i.ClientID == "" || len(i.Subject) > 512 {
		return fmt.Errorf("%w: invalid identity", ErrUnauthorized)
	}
	return nil
}
