package automation

import (
	"sync"
	"testing"

	"encore.app/automation/runtime"
	"encore.app/automation/store"
)

// Uses Encore's isolated PostgreSQL database and the actual authoring endpoints.
func TestPostgresBindingReattachReturnsPersistentIdentity(t *testing.T) {
	for _, scope := range []string{"device", "device_config"} {
		t.Run(scope, func(t *testing.T) {
			e := newEnv(t)
			rule := e.createRuleIn(serviceTestTenant, paramRule(e, "30"))
			id := e.dev1
			if scope == "device_config" {
				id = "qa-cfg-" + runtime.NewID()
			}
			req := &AttachBindingRequest{ScopeType: scope, ScopeID: id, Params: map[string]string{"limit": "30"}}
			first, err := e.svc().AttachBinding(e.ctx(), rule.ID, req)
			if err != nil {
				t.Fatal(err)
			}
			if first.ID == "" || first.CreatedAt.IsZero() {
				t.Fatalf("initial persistent identity absent: %+v", first)
			}
			detached, err := e.svc().DetachBinding(e.ctx(), first.ID)
			if err != nil || detached.Enabled || detached.DetachedAt == nil {
				t.Fatalf("detach: %+v %v", detached, err)
			}
			req.Params = map[string]string{"limit": "40"}
			again, err := e.svc().AttachBinding(e.ctx(), rule.ID, req)
			if err != nil {
				t.Fatal(err)
			}
			if again.ID != first.ID || again.CreatedAt.IsZero() || !again.CreatedAt.Equal(first.CreatedAt) || !again.Enabled || again.DetachedAt != nil || again.Params["limit"] != "40" {
				t.Fatalf("reattach returned fabricated rather than durable row: first=%+v again=%+v", first, again)
			}
			rows, err := e.st.ListBindingsForRule(e.ctx(), serviceTestTenant, rule.ID)
			if err != nil || len(rows) != 1 || rows[0].ID != again.ID {
				t.Fatalf("stored binding count/identity: %+v %v", rows, err)
			}
			final, err := e.svc().DetachBinding(e.ctx(), again.ID)
			if err != nil || final.Enabled || final.DetachedAt == nil {
				t.Fatalf("returned identity cannot be detached: %+v %v", final, err)
			}
			repeated, err := e.svc().DetachBinding(e.ctx(), again.ID)
			if err != nil || repeated.Enabled {
				t.Fatalf("repeat detach: %+v %v", repeated, err)
			}
		})
	}
}

func TestPostgresBindingRepeatedAttachReturnsPersistentIdentity(t *testing.T) {
	e := newEnv(t)
	rule := e.createRuleIn(serviceTestTenant, paramRule(e, "30"))
	req := &AttachBindingRequest{ScopeType: "device", ScopeID: e.dev1}
	first, err := e.svc().AttachBinding(e.ctx(), rule.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.svc().AttachBinding(e.ctx(), rule.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("repeated attach identity changed: first=%+v second=%+v", first, second)
	}
}

func TestPostgresBindingConcurrentAttachReturnsOneIdentity(t *testing.T) {
	e := newEnv(t)
	rule := e.createRuleIn(serviceTestTenant, paramRule(e, "30"))
	const n = 6
	results := make(chan *BindingResponse, n)
	failures := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := e.svc().AttachBinding(e.ctx(), rule.ID, &AttachBindingRequest{ScopeType: "device", ScopeID: e.dev1})
			if err != nil {
				failures <- err
				return
			}
			results <- b
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	rows, err := e.st.ListBindingsForRule(e.ctx(), serviceTestTenant, rule.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("persistent rows=%d err=%v", len(rows), err)
	}
	count := 0
	for response := range results {
		count++
		if response.ID != rows[0].ID || response.CreatedAt.IsZero() {
			t.Fatalf("concurrent response is not persistent row: %+v, stored=%+v", response, rows[0])
		}
	}
	if count != n {
		t.Fatalf("responses=%d", count)
	}
}

func TestPostgresBindingUpsertIdentityRemainsTenantScoped(t *testing.T) {
	e := newEnv(t)
	r1 := e.createRule(paramRule(e, "30"))
	r2 := e.createRuleIn("other-"+runtime.NewID(), paramRule(e, "30"))
	b1 := &store.Binding{ID: runtime.NewID(), TenantID: r1.TenantID, RuleID: r1.ID, DeviceID: e.dev1, Revision: 1, Source: store.BindingSourceDevice, Enabled: true}
	b2 := &store.Binding{ID: runtime.NewID(), TenantID: r2.TenantID, RuleID: r2.ID, DeviceID: e.dev1, Revision: 1, Source: store.BindingSourceDevice, Enabled: true}
	for _, b := range []*store.Binding{b1, b2} {
		if err := e.st.UpsertBinding(e.ctx(), b); err != nil {
			t.Fatal(err)
		}
	}
	old := b1.ID
	again := *b1
	again.ID = runtime.NewID()
	again.Params = map[string]string{"limit": "50"}
	if err := e.st.UpsertBinding(e.ctx(), &again); err != nil {
		t.Fatal(err)
	}
	if again.ID != old || again.CreatedAt.IsZero() {
		t.Fatalf("upsert caller retains unpersisted identity: %+v", again)
	}
	other, err := e.st.GetBinding(e.ctx(), r2.TenantID, b2.ID)
	if err != nil || other.Params["limit"] != "" {
		t.Fatalf("other tenant modified: %+v %v", other, err)
	}
	if _, err := e.st.GetBinding(e.ctx(), r2.TenantID, again.ID); err != store.ErrNotFound {
		t.Fatalf("other tenant reads binding: %v", err)
	}
}
