package brokeraitools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func startPrivateDelegationRedis(t *testing.T) (*redis.Client, *Store) {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server unavailable; private Redis delegation tests not run")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve private Redis port:", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	cmd := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir(), "--daemonize", "no", "--loglevel", "warning")
	if err := cmd.Start(); err != nil {
		t.Fatal("start private Redis:", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	client := redis.NewClient(&redis.Options{Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), MaxRetries: 0})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if err := client.Ping(ctx).Err(); err == nil {
			return client, New(client)
		}
		if ctx.Err() != nil {
			t.Fatal("private Redis did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func delegationSubject(tenant, user string) Subject {
	return Subject{TenantID: tenant, UserID: user, Authority: "qa-authority", SessionToken: "synthetic-parent-session-secret"}
}

func issueTestGrant(t *testing.T, store *Store, subject Subject) Issued {
	t.Helper()
	grant, err := store.Issue(context.Background(), subject, []string{"device-a"}, []string{Telemetry})
	if err != nil {
		t.Fatal("issue test grant:", err)
	}
	return grant
}

func TestDelegationConcurrentBudgetIsAtomic(t *testing.T) {
	_, store := startPrivateDelegationRedis(t)
	grant := issueTestGrant(t, store, delegationSubject("tenant-a", "user-a"))
	const callers = 100
	start := make(chan struct{})
	var success, budget, unexpected atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.Consume(context.Background(), grant.Token, Telemetry, "device-a")
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, ErrBudget):
				budget.Add(1)
			default:
				unexpected.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if success.Load() != MaxCalls || budget.Load() != callers-MaxCalls || unexpected.Load() != 0 {
		t.Fatalf("atomic budget mismatch: success=%d budget=%d unexpected=%d", success.Load(), budget.Load(), unexpected.Load())
	}
}

func TestDelegationInvalidScopeAndDeviceDoNotSpendBudget(t *testing.T) {
	_, store := startPrivateDelegationRedis(t)
	grant := issueTestGrant(t, store, delegationSubject("tenant-a", "user-a"))
	for _, tc := range []struct{ operation, device string }{
		{"commands.submit", "device-a"}, {"authorize", "device-a"}, {Telemetry, "device-b"}, {"", "device-a"},
	} {
		if _, err := store.Consume(context.Background(), grant.Token, tc.operation, tc.device); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("Consume(%q,%q) error=%v, want unauthorized", tc.operation, tc.device, err)
		}
	}
	for i := 0; i < MaxCalls; i++ {
		if _, err := store.Consume(context.Background(), grant.Token, Telemetry, "device-a"); err != nil {
			t.Fatalf("valid call %d lost budget after rejected calls: %v", i+1, err)
		}
	}
	if _, err := store.Consume(context.Background(), grant.Token, Telemetry, "device-a"); !errors.Is(err, ErrBudget) {
		t.Fatalf("21st valid call error=%v, want budget", err)
	}
}

func TestDelegationRevocationAndWrongOwner(t *testing.T) {
	_, store := startPrivateDelegationRedis(t)
	owner := delegationSubject("tenant-a", "user-a")
	grant := issueTestGrant(t, store, owner)
	wrong := delegationSubject("tenant-a", "user-b")
	if err := store.Revoke(context.Background(), wrong, grant.ID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong owner revoke error=%v, want unauthorized", err)
	}
	if _, err := store.Consume(context.Background(), grant.Token, Telemetry, "device-a"); err != nil {
		t.Fatalf("wrong-owner revoke changed grant: %v", err)
	}
	if err := store.Revoke(context.Background(), owner, grant.ID); err != nil {
		t.Fatal("owner revoke:", err)
	}
	if _, err := store.Consume(context.Background(), grant.Token, Telemetry, "device-a"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("consume after revoke error=%v, want unauthorized", err)
	}
}

func TestDelegationExpiryUsesRedisTTLAndExpiredIndexIsReclaimed(t *testing.T) {
	client, store := startPrivateDelegationRedis(t)
	owner := delegationSubject("tenant-a", "user-a")
	grants := make([]Issued, MaxUserGrants)
	for i := range grants {
		grants[i] = issueTestGrant(t, store, owner)
	}
	if _, err := store.Issue(context.Background(), owner, []string{"device-a"}, []string{Telemetry}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("ninth user grant error=%v, want capacity", err)
	}
	for _, grant := range grants {
		key, err := tokenKey(grant.Token)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.PExpire(context.Background(), key, 20*time.Millisecond).Err(); err != nil {
			t.Fatal("shorten private grant TTL:", err)
		}
		for _, index := range []string{userKey(owner), tenantKey(owner)} {
			if err := client.ZAdd(context.Background(), index, redis.Z{Score: float64(time.Now().Add(-time.Second).UnixMilli()), Member: key}).Err(); err != nil {
				t.Fatal("mark private index entry expired:", err)
			}
		}
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := store.Issue(context.Background(), owner, []string{"device-a"}, []string{Telemetry}); err != nil {
		t.Fatalf("issue after Redis expiry/index reclamation: %v", err)
	}
	if _, err := store.Consume(context.Background(), grants[0].Token, Telemetry, "device-a"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired token consume error=%v, want unauthorized", err)
	}
}

func TestDelegationTenantCapacityAndMalformedCredentials(t *testing.T) {
	_, store := startPrivateDelegationRedis(t)
	for i := 0; i < MaxTenantGrants; i++ {
		subject := delegationSubject("tenant-cap", fmt.Sprintf("u%04d", i))
		if _, err := store.Issue(context.Background(), subject, []string{"device-a"}, []string{Telemetry}); err != nil {
			t.Fatalf("tenant grant %d/%d: %v", i+1, MaxTenantGrants, err)
		}
	}
	if _, err := store.Issue(context.Background(), delegationSubject("tenant-cap", "over-cap"), []string{"device-a"}, []string{Telemetry}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("tenant cap error=%v, want capacity", err)
	}
	for _, token := range []string{"", "tpai_%%%", "tpai_abc", strings.Repeat("x", 48)} {
		if _, err := store.Consume(context.Background(), token, Telemetry, "device-a"); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("malformed token %q error=%v, want unauthorized", token, err)
		}
	}
	if _, err := store.Issue(context.Background(), delegationSubject("tenant-x", "user-x"), []string{"device-a"}, []string{"commands.submit"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown scope issue error=%v, want invalid", err)
	}
	if _, err := store.Issue(context.Background(), delegationSubject("tenant-x", "user-x"), []string{"device/#"}, []string{Telemetry}); !errors.Is(err, ErrInvalid) {
		t.Errorf("wildcard device issue error=%v, want invalid", err)
	}
}

func TestDelegationNeverSerializesParentSession(t *testing.T) {
	_, store := startPrivateDelegationRedis(t)
	subject := delegationSubject("tenant-a", "user-a")
	issued := issueTestGrant(t, store, subject)
	credential, err := store.Consume(context.Background(), issued.Token, Telemetry, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"issued": issued, "credential": credential, "subject": subject} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal("marshal", name, ":", err)
		}
		if strings.Contains(string(encoded), subject.SessionToken) {
			t.Errorf("%s JSON leaked parent session", name)
		}
	}
	if strings.Contains(issued.Token, subject.SessionToken) {
		t.Fatal("opaque token unexpectedly contains parent session")
	}
}
