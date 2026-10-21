package thingmodel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
	"github.com/google/uuid"
)

func TestPostgresRepositoryCreatePublishReplayAndTenantIsolation(t *testing.T) {
	ctx := context.Background()
	repo := newPostgresRepository()
	service := app.New(repo, uuid.NewString)
	principal := postgresPrincipal()

	created, err := service.Create(ctx, principal, "create-model-001", core.Model{Name: "Postgres native model"})
	if err != nil {
		t.Fatal(err)
	}
	modelID := core.ModelID(created.ModelID)
	replay, err := service.Create(ctx, principal, "create-model-001", core.Model{Name: "Postgres native model"})
	if err != nil || !replay.Replayed || replay.ModelID != created.ModelID {
		t.Fatalf("create replay = %+v, %v", replay, err)
	}

	published, err := service.Publish(ctx, principal, "publish-model-001", modelID, created.DraftRevision, "first")
	if err != nil || published.PublishedRevision == nil || *published.PublishedRevision != 1 {
		t.Fatalf("publish = %+v, %v", published, err)
	}
	second, err := service.Publish(ctx, principal, "publish-model-002", modelID, created.DraftRevision, "same draft, new publication")
	if err != nil || second.PublishedRevision == nil || *second.PublishedRevision != 2 {
		t.Fatalf("republish unchanged draft = %+v, %v", second, err)
	}
	versions, err := service.Versions(ctx, principal, modelID)
	if err != nil || len(versions) != 2 || versions[0].Snapshot.Revision != 2 || versions[1].Snapshot.Revision != 1 {
		t.Fatalf("versions must be newest first: %+v, %v", versions, err)
	}
	version, err := service.Version(ctx, principal, modelID, 1)
	if err != nil || version.Snapshot.Revision != 1 || version.Changelog != "first" {
		t.Fatalf("version 1 = %+v, %v", version, err)
	}

	otherTenant := postgresPrincipal()
	if _, err := repo.Get(ctx, otherTenant.TenantID, modelID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("cross-tenant Get = %v, want not found", err)
	}
	if _, err := repo.Version(ctx, otherTenant.TenantID, modelID, 1); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("cross-tenant Version = %v, want not found", err)
	}
	if _, err := repo.Get(ctx, "not-a-uuid", modelID); !errors.Is(err, app.ErrInvalid) {
		t.Fatalf("bad tenant UUID = %v, want invalid", err)
	}
	if _, err := repo.Versions(ctx, principal.TenantID, "not-a-uuid"); !errors.Is(err, app.ErrInvalid) {
		t.Fatalf("bad model UUID = %v, want invalid", err)
	}
}

func TestPostgresResourcesCASIdempotencyTenantAndPublishedSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := newPostgresRepository()
	service := app.New(repo, uuid.NewString)
	principal := postgresPrincipal()
	created, err := service.Create(ctx, principal, "resources-create-001", core.Model{Name: "resources model", Fields: []core.Field{}})
	if err != nil {
		t.Fatal(err)
	}
	modelID := core.ModelID(created.ModelID)
	initial, err := service.Resources(ctx, principal, modelID)
	if err != nil || initial.Revision != 0 || len(initial.Document.Profile.Tags) != 0 {
		t.Fatalf("initial resources = %+v %v", initial, err)
	}

	doc := app.ResourcesDocument{Profile: app.ResourceProfile{Author: "sensor team", Tags: []string{"sensor", "v2"}, Cover: "/assets/cover.png", Version: "2.1", Brand: "North", ModelNumber: "N-42"}, WebChartConfig: json.RawMessage(` { "zeta": 1e0, "nested": { "b": [ { "k": "v" }, 2 ], "a": true }, "alpha": 2 } `), AppChartConfig: json.RawMessage(`{"layout":"mobile","panels":[{"id":"a","axes":{"right":false,"left":true}}]}`)}
	saved, err := service.UpdateResources(ctx, principal, "resources-save-001", modelID, 0, doc)
	if err != nil || saved.ResourcesRevision == nil || *saved.ResourcesRevision != 1 {
		t.Fatalf("save resources = %+v %v", saved, err)
	}
	replay, err := service.UpdateResources(ctx, principal, "resources-save-001", modelID, 0, doc)
	if err != nil || !replay.Replayed || replay.ResourcesRevision == nil || *replay.ResourcesRevision != 1 {
		t.Fatalf("resource replay = %+v %v", replay, err)
	}
	var auditCount, outboxCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM thing_model_audit_log WHERE tenant_id=$1 AND target_id=$2 AND action='thingmodel.resources.updated'`, string(principal.TenantID), string(modelID)).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM thing_model_outbox WHERE tenant_id=$1 AND model_id=$2 AND operation='resources-updated'`, string(principal.TenantID), string(modelID)).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || outboxCount != 1 {
		t.Fatalf("resource save audit/outbox counts = %d/%d, want one each", auditCount, outboxCount)
	}
	changed := doc
	changed.Profile.Author = "different"
	if _, err := service.UpdateResources(ctx, principal, "resources-save-001", modelID, 0, changed); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("same key with different payload = %v", err)
	}
	if _, err := service.UpdateResources(ctx, principal, "resources-save-stale", modelID, 0, changed); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("stale resource revision = %v", err)
	}
	otherTenant := postgresPrincipal()
	if _, err := service.Resources(ctx, otherTenant, modelID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("cross-tenant resources = %v", err)
	}

	expectedResourceRevision := uint64(1)
	if _, err := service.PublishWithResources(ctx, principal, "resources-publish-001", modelID, created.DraftRevision, "resource snapshot", &expectedResourceRevision); err != nil {
		t.Fatal(err)
	}
	changed.Profile.Author = "after publish"
	changed.WebChartConfig = nil
	changed.AppChartConfig = nil
	if _, err := service.UpdateResources(ctx, principal, "resources-save-002", modelID, 1, changed); err != nil {
		t.Fatal(err)
	}
	current, err := service.Resources(ctx, principal, modelID)
	if err != nil || current.Revision != 2 || len(current.Document.WebChartConfig) != 0 || len(current.Document.AppChartConfig) != 0 {
		t.Fatalf("explicit chart null did not clear current resources: %+v %v", current, err)
	}
	version, err := service.Version(ctx, principal, modelID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var savedChart map[string]any
	_ = json.Unmarshal(version.Resources.Document.WebChartConfig, &savedChart)
	if version.Resources.Revision != 1 || version.Resources.Document.Profile.Author != "sensor team" || savedChart["alpha"] == nil || savedChart["nested"] == nil || savedChart["zeta"] == nil {
		t.Fatalf("published resources changed after draft edit: %+v", version.Resources)
	}
}

func TestPostgresResourceAndModelWritesRollbackTogether(t *testing.T) {
	ctx := context.Background()
	repo := newPostgresRepository()
	service := app.New(repo, uuid.NewString)
	principal := postgresPrincipal()
	created, err := service.Create(ctx, principal, "atomic-res-create", core.Model{Name: "before", Fields: []core.Field{}})
	if err != nil {
		t.Fatal(err)
	}
	modelID := core.ModelID(created.ModelID)
	scope := app.ReceiptScope{TenantID: principal.TenantID, SubjectID: principal.SubjectID, Operation: "test-resource-rollback", Key: "atomic-res-rollback", PayloadHash: strings.Repeat("d", 64)}
	_, err = repo.Execute(ctx, scope, func(tx app.Transaction) (app.Result, error) {
		record, e := tx.Get(ctx, principal.TenantID, modelID)
		if e != nil {
			return app.Result{}, e
		}
		record.Model.Name = "after"
		record.Model.DraftRevision++
		if e = tx.Update(ctx, record); e != nil {
			return app.Result{}, e
		}
		_, e = tx.SaveResources(ctx, principal.TenantID, core.ModelID(uuid.NewString()), 0, app.ResourcesDocument{Profile: app.ResourceProfile{Tags: []string{}}})
		if e == nil {
			return app.Result{}, errors.New("expected resource update to fail")
		}
		return app.Result{}, e
	})
	if !errors.Is(err, app.ErrConflict) {
		t.Fatalf("transaction error = %v", err)
	}
	stored, err := service.Get(ctx, principal, modelID)
	if err != nil || stored.Model.Name != "before" || stored.Model.DraftRevision != 1 {
		t.Fatalf("semantic write survived rollback: %+v %v", stored, err)
	}
	resources, err := service.Resources(ctx, principal, modelID)
	if err != nil || resources.Revision != 0 {
		t.Fatalf("resources changed after rollback: %+v %v", resources, err)
	}
	var receipts int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM thing_model_command_receipts WHERE tenant_id=$1 AND subject_id=$2 AND operation=$3 AND idempotency_key=$4`, string(principal.TenantID), principal.SubjectID, scope.Operation, scope.Key).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("receipt persisted after rollback: count=%d err=%v", receipts, err)
	}
}

func TestPostgresResourceWriteRollsBackWhenOutboxFails(t *testing.T) {
	ctx := context.Background()
	repo := newPostgresRepository()
	service := app.New(repo, uuid.NewString)
	principal := postgresPrincipal()
	created, err := service.Create(ctx, principal, "outbox-res-create", core.Model{Name: "resource outbox rollback", Fields: []core.Field{}})
	if err != nil {
		t.Fatal(err)
	}
	modelID := core.ModelID(created.ModelID)
	doc := app.ResourcesDocument{Profile: app.ResourceProfile{Author: "must rollback", Tags: []string{}}}
	scope := app.ReceiptScope{TenantID: principal.TenantID, SubjectID: principal.SubjectID, Operation: "test-resource-outbox", Key: "resource-outbox-failure", PayloadHash: strings.Repeat("e", 64)}
	_, err = repo.Execute(ctx, scope, func(tx app.Transaction) (app.Result, error) {
		if _, e := tx.Get(ctx, principal.TenantID, modelID); e != nil {
			return app.Result{}, e
		}
		if _, e := tx.SaveResources(ctx, principal.TenantID, modelID, 0, doc); e != nil {
			return app.Result{}, e
		}
		return app.Result{}, tx.Emit(ctx, app.Event{ID: "invalid", Operation: "resources-updated", ModelID: modelID, TenantID: principal.TenantID, DraftRevision: created.DraftRevision})
	})
	if !errors.Is(err, app.ErrInvalid) {
		t.Fatalf("outbox failure = %v", err)
	}
	resources, err := service.Resources(ctx, principal, modelID)
	if err != nil || resources.Revision != 0 || resources.Document.Profile.Author != "" {
		t.Fatalf("resource survived failed outbox: %+v %v", resources, err)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM thing_model_command_receipts WHERE tenant_id=$1 AND subject_id=$2 AND operation=$3 AND idempotency_key=$4`, string(principal.TenantID), principal.SubjectID, scope.Operation, scope.Key).Scan(&count); err != nil || count != 0 {
		t.Fatalf("receipt survived failed outbox: count=%d err=%v", count, err)
	}
}

func TestPostgresRepositoryKeepsExpiredReceiptAsTombstone(t *testing.T) {
	ctx := context.Background()
	repo := newPostgresRepository()
	scope := app.ReceiptScope{
		TenantID: core.TenantID(uuid.NewString()), SubjectID: uuid.NewString(),
		Operation: "create", Key: "expired-key-001", PayloadHash: strings.Repeat("a", 64),
	}
	calls := 0
	if _, err := repo.Execute(ctx, scope, func(app.Transaction) (app.Result, error) {
		calls++
		return app.Result{Status: "SAVED"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE thing_model_command_receipts SET expires_at=$1 WHERE tenant_id=$2 AND subject_id=$3 AND operation=$4 AND idempotency_key=$5`, time.Now().UTC().Add(-time.Minute), string(scope.TenantID), scope.SubjectID, scope.Operation, scope.Key); err != nil {
		t.Fatalf("expire receipt: %v", err)
	}
	if _, err := repo.Execute(ctx, scope, func(app.Transaction) (app.Result, error) {
		calls++
		return app.Result{Status: "SAVED"}, nil
	}); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("expired receipt retry = %v, want conflict", err)
	}
	if calls != 1 {
		t.Fatalf("expired receipt callback ran %d times, want once", calls)
	}
	var resultIsNull bool
	if err := db.QueryRow(ctx, `SELECT result IS NULL FROM thing_model_command_receipts WHERE tenant_id=$1 AND subject_id=$2 AND operation=$3 AND idempotency_key=$4`, string(scope.TenantID), scope.SubjectID, scope.Operation, scope.Key).Scan(&resultIsNull); err != nil || !resultIsNull {
		t.Fatalf("expired receipt result should be cleared: null=%v err=%v", resultIsNull, err)
	}
}

func TestPostgresRepositoryRollbackLeavesNoPartialWrites(t *testing.T) {
	ctx := context.Background()
	repo := newPostgresRepository()
	tenantID, subjectID := uuid.NewString(), uuid.NewString()
	model := core.Model{ID: core.ModelID(uuid.NewString()), TenantID: core.TenantID(tenantID), Name: "atomic rollback", Fields: []core.Field{}, DraftRevision: 1}
	record := app.Record{Model: model, Lifecycle: "DRAFT"}
	eventID := uuid.NewString()
	scope := app.ReceiptScope{TenantID: model.TenantID, SubjectID: subjectID, Operation: "test-rollback", Key: "rollback-key-001", PayloadHash: strings.Repeat("b", 64)}
	_, err := repo.Execute(ctx, scope, func(tx app.Transaction) (app.Result, error) {
		if err := tx.Insert(ctx, record); err != nil {
			return app.Result{}, err
		}
		if err := tx.Audit(ctx, app.Audit{ID: uuid.NewString(), SubjectID: subjectID, Operation: "created", After: record}); err != nil {
			return app.Result{}, err
		}
		event := app.Event{ID: eventID, Operation: "created", ModelID: model.ID, TenantID: model.TenantID, DraftRevision: 1}
		if err := tx.Emit(ctx, event); err != nil {
			return app.Result{}, err
		}
		// The second insert violates the outbox primary key after model, audit,
		// and the first outbox row have been written in the open transaction.
		if err := tx.Emit(ctx, event); err != nil {
			return app.Result{}, err
		}
		return app.Result{Status: "SAVED"}, nil
	})
	if !errors.Is(err, app.ErrConflict) {
		t.Fatalf("duplicate outbox event = %v, want conflict", err)
	}
	if _, err := repo.Get(ctx, model.TenantID, model.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("model after rollback = %v, want not found", err)
	}
	for _, query := range []struct {
		label string
		sql   string
	}{
		{"audit", `SELECT COUNT(*) FROM thing_model_audit_log WHERE tenant_id=$1 AND target_id=$2`},
		{"outbox", `SELECT COUNT(*) FROM thing_model_outbox WHERE tenant_id=$1 AND model_id=$2`},
		{"receipt", `SELECT COUNT(*) FROM thing_model_command_receipts WHERE tenant_id=$1 AND subject_id=$2 AND operation=$3 AND idempotency_key=$4`},
	} {
		var count int
		var err error
		switch query.label {
		case "receipt":
			err = db.QueryRow(ctx, query.sql, tenantID, subjectID, scope.Operation, scope.Key).Scan(&count)
		default:
			err = db.QueryRow(ctx, query.sql, tenantID, string(model.ID)).Scan(&count)
		}
		if err != nil || count != 0 {
			t.Fatalf("%s rows after rollback = %d, err=%v", query.label, count, err)
		}
	}
}

func TestPostgresRepositoryDoesNotExposeLegacyRows(t *testing.T) {
	ctx := context.Background()
	tenantID, modelID := uuid.NewString(), uuid.NewString()
	if _, err := db.Exec(ctx, `INSERT INTO thing_models(id,tenant_id,name,status,created_at,updated_at) VALUES($1,$2,$3,'DRAFT',NOW(),NOW())`, modelID, tenantID, "legacy-only-"+modelID); err != nil {
		t.Fatal(err)
	}
	if _, err := newPostgresRepository().Get(ctx, core.TenantID(tenantID), core.ModelID(modelID)); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("legacy row read = %v, want not found", err)
	}
	if _, total, err := newPostgresRepository().List(ctx, core.TenantID(tenantID), 20, 0, "legacy-only"); err != nil || total != 0 {
		t.Fatalf("legacy row appeared in list: total=%d err=%v", total, err)
	}
}

func postgresPrincipal() core.Principal {
	return core.Principal{SubjectID: uuid.NewString(), TenantID: core.TenantID(uuid.NewString()), Capabilities: []string{
		string(core.CapabilityRead), string(core.CapabilityWrite), string(core.CapabilityPublish),
	}}
}

func TestPostgresRepositoryReceiptScopeMatchesContract(t *testing.T) {
	scope := app.ReceiptScope{TenantID: core.TenantID(uuid.NewString()), SubjectID: uuid.NewString(), Operation: "publish", Key: "1234567", PayloadHash: strings.Repeat("c", 64)}
	if validReceiptScope(scope) {
		t.Fatal("7-byte idempotency key accepted")
	}
	scope.Key = "12345678"
	if !validReceiptScope(scope) {
		t.Fatal("8-byte idempotency key rejected")
	}
	scope.Key = strings.Repeat("x", 129)
	if validReceiptScope(scope) {
		t.Fatal("129-byte idempotency key accepted")
	}
}

func TestPostgresConcurrentRequestsCreateOnceAndCASSelectsOneWriter(t *testing.T) {
	ctx := context.Background()
	p := postgresPrincipal()
	s := app.New(newPostgresRepository(), uuid.NewString)
	const count = 8
	results := make(chan app.Result, count)
	errorsFound := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.Create(ctx, p, "same-concurrent-key", core.Model{Name: "concurrent create", Fields: []core.Field{}})
			if e != nil {
				errorsFound <- e
				return
			}
			results <- r
		}()
	}
	wg.Wait()
	close(results)
	close(errorsFound)
	for e := range errorsFound {
		t.Fatal(e)
	}
	var id string
	for r := range results {
		if id == "" {
			id = r.ModelID
		}
		if id != r.ModelID {
			t.Fatal("same key created multiple models")
		}
	}
	var modelCount, receiptCount, auditCount, eventCount int
	if e := db.QueryRow(ctx, `SELECT COUNT(*) FROM thing_models WHERE tenant_id=$1`, string(p.TenantID)).Scan(&modelCount); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(ctx, `SELECT COUNT(*) FROM thing_model_command_receipts WHERE tenant_id=$1`, string(p.TenantID)).Scan(&receiptCount); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(ctx, `SELECT COUNT(*) FROM thing_model_audit_log WHERE tenant_id=$1`, string(p.TenantID)).Scan(&auditCount); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(ctx, `SELECT COUNT(*) FROM thing_model_outbox WHERE tenant_id=$1`, string(p.TenantID)).Scan(&eventCount); e != nil {
		t.Fatal(e)
	}
	if modelCount != 1 || receiptCount != 1 || auditCount != 1 || eventCount != 1 {
		t.Fatalf("concurrent replay duplicated records: %d/%d/%d/%d", modelCount, receiptCount, auditCount, eventCount)
	}
	outcomes := make(chan error, 2)
	for _, name := range []string{"writer A", "writer B"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, e := s.Update(ctx, p, "cas-key-"+name, core.ModelID(id), 1, core.Model{Name: name, Fields: []core.Field{}})
			outcomes <- e
		}(name)
	}
	wg.Wait()
	close(outcomes)
	won, conflicts := 0, 0
	for e := range outcomes {
		if e == nil {
			won++
		} else if errors.Is(e, app.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if won != 1 || conflicts != 1 {
		t.Fatalf("expected one winner and one conflict: %d/%d", won, conflicts)
	}
}

func TestPostgresPublishedSnapshotCannotBeModified(t *testing.T) {
	ctx := context.Background()
	p := postgresPrincipal()
	s := app.New(newPostgresRepository(), uuid.NewString)
	r, e := s.Create(ctx, p, "immutable-create", core.Model{Name: "original snapshot", Fields: []core.Field{}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Publish(ctx, p, "immutable-publish", core.ModelID(r.ModelID), 1, "original"); e != nil {
		t.Fatal(e)
	}
	before, e := s.Version(ctx, p, core.ModelID(r.ModelID), 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `UPDATE thing_model_snapshots SET changelog='tampered' WHERE id=$1`, string(before.Snapshot.ID)); e == nil {
		t.Fatal("immutable snapshot update was accepted")
	}
	after, e := s.Version(ctx, p, core.ModelID(r.ModelID), 1)
	if e != nil || before.Snapshot.Checksum != after.Snapshot.Checksum || after.Changelog != "original" {
		t.Fatalf("immutable snapshot changed: %+v %v", after, e)
	}
}
