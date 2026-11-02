package thingmodel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"encore.app/thingmodel/app"
	"encore.app/thingmodel/core"
	"github.com/google/uuid"
)

func TestPostgresReceiptQueryOutcomeScopeAndTombstone(t *testing.T) {
	ctx := context.Background()
	p := postgresPrincipal()
	owner := postgresRepository{}
	service := app.New(owner, uuid.NewString)
	key := "receipt-query-" + uuid.NewString()
	observation, err := app.ReadReceipt(ctx, owner, p, "create", key, "")
	if err != nil || observation.State != app.ReceiptNotObserved || observation.Result != nil {
		t.Fatalf("before command: %#v %v", observation, err)
	}
	created, err := service.Create(ctx, p, key, core.Model{Name: "Receipt query " + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	observation, err = app.ReadReceipt(ctx, owner, p, "create", key, "")
	if err != nil || observation.State != app.ReceiptConfirmed || observation.Result == nil || observation.Result.ModelID != created.ModelID || !observation.Result.Replayed || len(observation.PayloadHash) != 64 || observation.RetainedUntil == nil {
		t.Fatalf("committed command: %#v %v", observation, err)
	}
	denied := p
	denied.Capabilities = []string{string(core.CapabilityRead)}
	if _, err := app.ReadReceipt(ctx, owner, denied, "create", key, ""); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("revoked writer queried committed receipt: %v", err)
	}
	writeOnly := p
	writeOnly.Capabilities = []string{string(core.CapabilityWrite)}
	if _, err := app.ReadReceipt(ctx, owner, writeOnly, "publish", key, ""); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("writer without publish queried publish receipt: %v", err)
	}
	hash := observation.PayloadHash
	if _, err := app.ReadReceipt(ctx, owner, p, "create", key, strings.Repeat("0", 64)); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("different digest query: %v", err)
	}
	if _, err := app.ReadReceipt(ctx, owner, p, "create", key, hash); err != nil {
		t.Fatal(err)
	}
	for _, foreign := range []core.Principal{postgresPrincipal(), {TenantID: p.TenantID, SubjectID: uuid.NewString(), Capabilities: p.Capabilities}} {
		observation, err := app.ReadReceipt(ctx, owner, foreign, "create", key, "")
		if err != nil || observation.State != app.ReceiptNotObserved || observation.Result != nil {
			t.Fatalf("foreign receipt leaked: %#v %v", observation, err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE thing_model_command_receipts SET expires_at=$1 WHERE tenant_id=$2 AND subject_id=$3 AND operation='create' AND idempotency_key=$4`, time.Now().UTC().Add(-time.Minute), string(p.TenantID), p.SubjectID, key); err != nil {
		t.Fatal(err)
	}
	observation, err = app.ReadReceipt(ctx, owner, p, "create", key, hash)
	if err != nil || observation.State != app.ReceiptExpired || observation.Result != nil || observation.PayloadHash != hash {
		t.Fatalf("expired: %#v %v", observation, err)
	}
	if _, err := service.Create(ctx, p, key, core.Model{Name: "Receipt query " + uuid.NewString()}); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("expired key allowed new effect: %v", err)
	}
}

func TestPostgresReceiptQueryWaitsForOriginalOwnerTransaction(t *testing.T) {
	ctx := context.Background()
	p := postgresPrincipal()
	key := "receipt-wait-" + uuid.NewString()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, receiptLockIdentity(p.TenantID, p.SubjectID, "create", key)); err != nil {
		t.Fatal(err)
	}
	// A short context cannot observe not_observed while an earlier owner holds
	// the same key transaction. A timeout must stay unknown, never evidence of absence.
	waiting, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	observation, err := app.ReadReceipt(waiting, postgresRepository{}, p, "create", key, "")
	if err == nil || observation.State != "" {
		t.Fatalf("in-flight transaction became absent: %#v %v", observation, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	observation, err = app.ReadReceipt(ctx, postgresRepository{}, p, "create", key, "")
	if err != nil || observation.State != app.ReceiptNotObserved {
		t.Fatalf("after rollback: %#v %v", observation, err)
	}
}

func TestPostgresReceiptQueryAfterOwnerRollback(t *testing.T) {
	ctx := context.Background()
	p := postgresPrincipal()
	owner := postgresRepository{}
	key := "receipt-rollback-" + uuid.NewString()
	modelID := core.ModelID(uuid.NewString())
	injected := errors.New("injected owner rollback")
	_, err := owner.Execute(ctx, app.ReceiptScope{TenantID: p.TenantID, SubjectID: p.SubjectID, Operation: "create", Key: key, PayloadHash: strings.Repeat("a", 64)}, func(tx app.Transaction) (app.Result, error) {
		if err := tx.Insert(ctx, app.Record{Model: core.Model{ID: modelID, TenantID: p.TenantID, Name: "Rollback receipt " + uuid.NewString(), DraftRevision: 1}, Lifecycle: "DRAFT"}); err != nil {
			return app.Result{}, err
		}
		return app.Result{}, injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("owner rollback returned %v", err)
	}
	observation, err := app.ReadReceipt(ctx, owner, p, "create", key, "")
	if err != nil || observation.State != app.ReceiptNotObserved {
		t.Fatalf("rolled-back mutation has receipt: %#v %v", observation, err)
	}
	if _, err := owner.Get(ctx, p.TenantID, modelID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("rolled-back model survived: %v", err)
	}
}

func TestPostgresReceiptQueryConcurrentOwnerCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := postgresPrincipal()
	owner := postgresRepository{}
	key := "receipt-concurrent-" + uuid.NewString()
	modelID := core.ModelID(uuid.NewString())
	locked := make(chan struct{})
	release := make(chan struct{})
	mutationDone := make(chan error, 1)
	go func() {
		_, err := owner.Execute(ctx, app.ReceiptScope{TenantID: p.TenantID, SubjectID: p.SubjectID, Operation: "create", Key: key, PayloadHash: strings.Repeat("b", 64)}, func(tx app.Transaction) (app.Result, error) {
			if err := tx.Insert(ctx, app.Record{Model: core.Model{ID: modelID, TenantID: p.TenantID, Name: "Concurrent receipt " + uuid.NewString(), DraftRevision: 1}, Lifecycle: "DRAFT"}); err != nil {
				return app.Result{}, err
			}
			close(locked)
			select {
			case <-release:
			case <-ctx.Done():
				return app.Result{}, ctx.Err()
			}
			return app.Result{Status: "SAVED", Lifecycle: "DRAFT", ModelID: string(modelID), DraftRevision: 1}, nil
		})
		mutationDone <- err
	}()
	select {
	case <-locked:
	case err := <-mutationDone:
		t.Fatalf("mutation failed before lock: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	type answer struct {
		observation app.ReceiptObservation
		err         error
	}
	queryDone := make(chan answer, 1)
	go func() {
		observation, err := app.ReadReceipt(ctx, owner, p, "create", key, strings.Repeat("b", 64))
		queryDone <- answer{observation, err}
	}()
	// A query that returns before release may never invent an absent receipt.
	select {
	case result := <-queryDone:
		close(release)
		t.Fatalf("query escaped in-flight mutation: %#v %v", result.observation, result.err)
	default:
	}
	close(release)
	if err := <-mutationDone; err != nil {
		t.Fatal(err)
	}
	result := <-queryDone
	if result.err != nil || result.observation.State != app.ReceiptConfirmed || result.observation.Result == nil || result.observation.Result.ModelID != string(modelID) {
		t.Fatalf("commit observation: %#v %v", result.observation, result.err)
	}
}
