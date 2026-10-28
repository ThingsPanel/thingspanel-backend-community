package authcore

import (
	"context"
	"errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryAccounts struct {
	mu       sync.Mutex
	user     User
	share    bool
	identity *Identity
	disabled bool
}

func (a *memoryAccounts) User(_ context.Context, key string, login bool) (User, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if key != a.user.ID && key != a.user.Email {
		return User{}, ErrUnauthorized
	}
	return a.user, nil
}
func (a *memoryAccounts) Flag(_ context.Context, name string) (bool, error) {
	return name == "shared_account" && a.share, nil
}
func (a *memoryAccounts) FindIdentity(_ context.Context, i Identity) (User, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.identity == nil || *a.identity != i || a.disabled {
		return User{}, ErrUnauthorized
	}
	return a.user, nil
}
func (a *memoryAccounts) Bind(_ context.Context, u User, i Identity) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.disabled {
		return ErrConflict
	}
	if u.ID != a.user.ID {
		return ErrConflict
	}
	a.identity = &i
	return nil
}
func (a *memoryAccounts) Disable(_ context.Context, _ User, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.disabled = true
	a.user.Version++
	return nil
}
func (a *memoryAccounts) Bindings(context.Context, User) ([]Binding, error) { return []Binding{}, nil }
func fixture(t *testing.T) (*Service, *memoryAccounts, *miniredis.Miniredis) {
	t.Helper()
	r := miniredis.RunT(t)
	hash, e := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if e != nil {
		t.Fatal(e)
	}
	a := &memoryAccounts{user: User{ID: "user-1", Email: "test@example.com", Status: "N", TenantID: "tenant-1", Authority: "TENANT_ADMIN", Password: string(hash)}}
	s := &Service{Accounts: a, Redis: redis.NewClient(&redis.Options{Addr: r.Addr()}), Key: []byte(strings.Repeat("s", 32)), TTL: time.Hour, MaxFailures: 3, LockDuration: time.Minute}
	t.Cleanup(func() { s.Redis.Close() })
	return s, a, r
}
func TestSessionLifecycle(t *testing.T) {
	s, a, r := fixture(t)
	ctx := context.Background()
	u, e := s.VerifyPassword(ctx, a.user.Email, "correct-password", "")
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.Issue(ctx, u)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Check(ctx, first.Token); e != nil {
		t.Fatal(e)
	}
	r.Set(PushKey(first.Token), "1")
	second, e := s.Issue(ctx, u)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Check(ctx, first.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("single login retained old session")
	}
	if r.Exists(PushKey(first.Token)) {
		t.Fatal("single login retained push eligibility")
	}
	a.share = true
	third, e := s.Issue(ctx, u)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Check(ctx, second.Token); e != nil {
		t.Fatal("shared login kicked existing device")
	}
	r.Set(PushKey(third.Token), "1")
	if s.Logout(ctx, third.Token) != nil {
		t.Fatal("logout failed")
	}
	if _, e = s.Check(ctx, third.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("logout resurrected")
	}
	if r.Exists(PushKey(third.Token)) {
		t.Fatal("push remains after logout")
	}
	if _, e = s.Check(ctx, second.Token); e != nil {
		t.Fatal("other device logged out")
	}
	a.Disable(ctx, u, "binding")
	if _, e = s.Check(ctx, second.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("version revocation bypass")
	}
}
func TestPasswordLockAndBindingDoesNotIssueSession(t *testing.T) {
	s, a, r := fixture(t)
	ctx := context.Background()
	session, e := s.Issue(ctx, a.user)
	if e != nil {
		t.Fatal(e)
	}
	before := r.Keys()
	id := Identity{Issuer: "https://issuer", Subject: "subject", Provider: "work", ClientID: "client"}
	if e = s.Bind(ctx, session.Token, "correct-password", "", id); e != nil {
		t.Fatal(e)
	}
	if len(r.Keys()) != len(before) {
		t.Fatal("bind created another session")
	}
	if _, e = s.Check(ctx, session.Token); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		_, e = s.VerifyPassword(ctx, a.user.Email, "wrong", "")
		if !errors.Is(e, ErrUnauthorized) {
			t.Fatal(e)
		}
	}
	if _, e = s.VerifyPassword(ctx, a.user.Email, "correct-password", ""); !errors.Is(e, ErrRateLimit) {
		t.Fatal("locked account accepted")
	}
}
func TestInvalidClaimsAndAccountState(t *testing.T) {
	s, a, r := fixture(t)
	ctx := context.Background()
	tests := []struct {
		name   string
		change func(*Claims)
	}{
		{"missing expiry", func(c *Claims) { c.ExpiresAt = nil }}, {"expired", func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Second)) }},
		{"tenant", func(c *Claims) { c.TenantID = "other" }}, {"role", func(c *Claims) { c.Authority = "SYS_ADMIN" }}, {"version", func(c *Claims) { c.Version = 99 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Claims{ID: a.user.ID, Email: a.user.Email, TenantID: a.user.TenantID, Authority: a.user.Authority, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
			tt.change(&c)
			raw, e := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.Key)
			if e != nil {
				t.Fatal(e)
			}
			r.Set(raw, "1")
			if _, e = s.Check(ctx, raw); !errors.Is(e, ErrUnauthorized) {
				t.Fatal("invalid token accepted", e)
			}
		})
	}
	session, e := s.Issue(ctx, a.user)
	if e != nil {
		t.Fatal(e)
	}
	a.user.Status = "F"
	if _, e = s.Check(ctx, session.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("frozen user accepted")
	}
}
func TestConcurrentTicketAndCodeConsumption(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	secret, e := Random()
	if e != nil {
		t.Fatal(e)
	}
	if s.Put(ctx, "ticket", secret, map[string]string{"purpose": "login"}, time.Minute) != nil {
		t.Fatal("put")
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var result map[string]string
			if s.Take(ctx, "ticket", secret, &result) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("ticket replay", wins.Load())
	}
	if s.Claim(ctx, "wechat:app", "code", time.Minute) != nil {
		t.Fatal("first code rejected")
	}
	if !errors.Is(s.Claim(ctx, "wechat:app", "code", time.Minute), ErrUnauthorized) {
		t.Fatal("code reused across purposes")
	}
}
func TestConflictingHeadersAndUnavailableStorage(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Authorization", "Bearer a")
	r.Header.Set("x-token", "b")
	if _, e := Bearer(r); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("conflicting credentials accepted")
	}
	s, a, redisServer := fixture(t)
	session, e := s.Issue(context.Background(), a.user)
	if e != nil {
		t.Fatal(e)
	}
	redisServer.Close()
	if _, e = s.Check(context.Background(), session.Token); !errors.Is(e, ErrUnavailable) {
		t.Fatal("storage fault not distinguished", e)
	}
}
func TestTicketCannotCrossBrowserOrPurpose(t *testing.T) {
	s, _, _ := fixture(t)
	h := &HTTP{Runtime: &Runtime{Core: s, Providers: map[string]*Provider{}, Origin: "https://platform"}}
	for _, tc := range []struct{ browser, purpose string }{{"different", "login"}, {strings.Repeat("b", 43), "bind"}} {
		secret, _ := Random()
		s.Put(context.Background(), "ticket", secret, resultTicket{ExpiresAt: time.Now().Add(time.Minute).Unix(), Browser: Digest(strings.Repeat("b", 43)), Purpose: tc.purpose}, time.Minute)
		r := httptest.NewRequest("POST", "https://platform/api/v1/auth/external/exchange", strings.NewReader(`{"ticket":"`+secret+`"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Cookie", "__Host-tp-auth="+tc.browser)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("cross browser/purpose accepted", w.Code)
		}
	}
}

func TestSingleLoginRemovesPreMigrationPushEligibility(t *testing.T) {
	s, a, r := fixture(t)
	r.Set(a.user.Email+"_token", "legacy-session")
	r.Set("legacy-session", "1")
	r.Set(PushKey("legacy-session"), "1")
	if _, e := s.Issue(context.Background(), a.user); e != nil {
		t.Fatal(e)
	}
	if r.Exists("legacy-session") || r.Exists(PushKey("legacy-session")) {
		t.Fatal("legacy eligibility survived replacement")
	}
}

type actorAccounts struct {
	*memoryAccounts
	actor User
}

func (a *actorAccounts) User(ctx context.Context, key string, login bool) (User, error) {
	if key == a.actor.ID {
		return a.actor, nil
	}
	return a.memoryAccounts.User(ctx, key, login)
}
func TestDelegatedSessionRevokesWithActorAndCannotBind(t *testing.T) {
	s, a, _ := fixture(t)
	accounts := &actorAccounts{memoryAccounts: a, actor: User{ID: "admin", Status: "N", Authority: "SYS_ADMIN", Version: 2}}
	s.Accounts = accounts
	u := a.user
	u.Delegated = true
	u.ActorID = "admin"
	u.ActorVersion = 2
	session, e := s.Issue(context.Background(), u)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Check(context.Background(), session.Token); e != nil {
		t.Fatal(e)
	}
	if e = s.Bind(context.Background(), session.Token, "correct-password", "", Identity{}); !errors.Is(e, ErrForbidden) {
		t.Fatal("delegated binding accepted", e)
	}
	accounts.actor.Version++
	if _, e = s.Check(context.Background(), session.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("actor revocation bypass", e)
	}
}

func TestRefreshCannotReviveLoggedOutSession(t *testing.T) {
	s, a, r := fixture(t)
	a.share = true
	ctx := context.Background()
	first, e := s.Issue(ctx, a.user)
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.Check(ctx, first.Token)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Logout(ctx, first.Token); e != nil {
		t.Fatal(e)
	}
	before := len(r.Keys())
	if _, e = s.issue(ctx, u, first.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("logout raced refresh", e)
	}
	if len(r.Keys()) != before {
		t.Fatal("rejected refresh wrote a session")
	}
	first, e = s.Issue(ctx, a.user)
	if e != nil {
		t.Fatal(e)
	}
	r.Set(PushKey(first.Token), "1")
	second, e := s.Refresh(ctx, first.Token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Check(ctx, first.Token); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("rotated session valid")
	}
	if r.Exists(PushKey(first.Token)) {
		t.Fatal("rotated push retained")
	}
	if _, e = s.Check(ctx, second.Token); e != nil {
		t.Fatal(e)
	}
}

func TestLogoutDoesNotDeleteArbitraryRedisKey(t *testing.T) {
	s, _, r := fixture(t)
	r.Set("protected-key", "value")
	if e := s.Logout(context.Background(), "protected-key"); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	if !r.Exists("protected-key") {
		t.Fatal("arbitrary redis key deleted")
	}
}
