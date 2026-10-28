package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"encore.app/thingmodel/core"
)

func TestPublishedResourcesPreserveEmptyTagsAcrossVersionReads(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	principal := writerPrincipal("tenant-a", "user-a")
	resources := ResourcesDocument{Profile: ResourceProfile{Author: "QA", Tags: []string{}}}

	created, err := service.CreateWithResources(ctx, principal, "create-empty-tags", core.Model{Name: "Empty tags"}, &resources)
	if err != nil {
		t.Fatal(err)
	}
	published, err := service.PublishWithResources(ctx, principal, "publish-empty-tags", core.ModelID(created.ModelID), 1, "empty tags", created.ResourcesRevision)
	if err != nil || published.PublishedRevision == nil || *published.PublishedRevision != 1 {
		t.Fatalf("publish result=%+v err=%v", published, err)
	}

	assertEmptyTagsJSON := func(label string, publication Publication) {
		t.Helper()
		if publication.Resources.Document.Profile.Tags == nil || len(publication.Resources.Document.Profile.Tags) != 0 {
			t.Fatalf("%s tags must remain a non-nil empty slice, got %#v", label, publication.Resources.Document.Profile.Tags)
		}
		encoded, err := json.Marshal(publication.Resources.Document.Profile)
		if err != nil {
			t.Fatalf("marshal %s profile: %v", label, err)
		}
		var profile map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &profile); err != nil {
			t.Fatalf("unmarshal %s profile: %v", label, err)
		}
		if string(profile["tags"]) != "[]" {
			t.Fatalf("%s profile.tags JSON = %s, want []", label, profile["tags"])
		}
	}

	version, err := service.Version(ctx, principal, core.ModelID(created.ModelID), 1)
	if err != nil {
		t.Fatal(err)
	}
	assertEmptyTagsJSON("Version", version)

	versions, err := service.Versions(ctx, principal, core.ModelID(created.ModelID))
	if err != nil || len(versions) != 1 {
		t.Fatalf("Versions count=%d err=%v", len(versions), err)
	}
	assertEmptyTagsJSON("Versions", versions[0])

	// Reads must clone the persisted publication without mutating its stored source.
	stored := repo.versions[memoryRecordKey("tenant-a", created.ModelID)][1].Resources.Document.Profile.Tags
	if stored == nil || len(stored) != 0 {
		t.Fatalf("stored source tags changed after reads: %#v", stored)
	}
}

func TestCloneResourcesPreservesNilAndCopiesNonEmptyTags(t *testing.T) {
	if got := cloneResources(ResourcesDocument{}).Profile.Tags; got != nil {
		t.Fatalf("nil tags must remain nil before normalization, got %#v", got)
	}

	source := ResourcesDocument{Profile: ResourceProfile{Tags: []string{"alpha"}}}
	cloned := cloneResources(source)
	if len(cloned.Profile.Tags) != 1 || cloned.Profile.Tags[0] != "alpha" {
		t.Fatalf("unexpected cloned tags: %#v", cloned.Profile.Tags)
	}
	cloned.Profile.Tags[0] = "changed"
	if source.Profile.Tags[0] != "alpha" {
		t.Fatalf("mutating cloned tags changed source: %#v", source.Profile.Tags)
	}
}

func TestCreateUpdatePublishAndReadVersions(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	principal := writerPrincipal("tenant-a", "user-a")

	created, err := service.Create(ctx, principal, "create-001", core.Model{ID: "spoofed", TenantID: "tenant-b", Name: "Pump"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ModelID == "spoofed" || created.DraftRevision != 1 || created.PublishedRevision != nil || created.Lifecycle != "DRAFT" || created.Status != "SAVED" || created.ConsumerState != "NOT_REQUESTED" {
		t.Fatalf("unexpected created result: %+v", created)
	}
	model, err := service.Get(ctx, principal, core.ModelID(created.ModelID))
	if err != nil || model.Model.TenantID != "tenant-a" || model.Model.ID != core.ModelID(created.ModelID) {
		t.Fatalf("create must use trusted tenant and allocated ID: record=%+v err=%v", model, err)
	}

	replacement := model.Model
	replacement.Name = "Pump v2"
	updated, err := service.Update(ctx, principal, "update-001", model.Model.ID, 1, replacement)
	if err != nil || updated.DraftRevision != 2 || updated.Status != "SAVED" || updated.ConsumerState != "NOT_REQUESTED" {
		t.Fatalf("update result=%+v err=%v", updated, err)
	}

	noop, err := service.Update(ctx, principal, "update-noop", model.Model.ID, 2, replacement)
	if err != nil || noop.DraftRevision != 2 || noop.Status != "SAVED" {
		t.Fatalf("semantic no-op advanced revision: result=%+v err=%v", noop, err)
	}

	published, err := service.Publish(ctx, principal, "publish-001", model.Model.ID, 2, "first stable version")
	if err != nil || published.Status != "PENDING_CONSUMER" || published.Lifecycle != "PUBLISHED" || published.PublishedRevision == nil || *published.PublishedRevision != 1 || published.ConsumerState != "PENDING" {
		t.Fatalf("publish result=%+v err=%v", published, err)
	}
	if published.ConsumerState == "APPLIED" {
		t.Fatal("application must not claim a consumer applied the publication")
	}

	secondPublish, err := service.Publish(ctx, principal, "publish-002", model.Model.ID, 2, "republish unchanged draft")
	if err != nil || secondPublish.Status != "PENDING_CONSUMER" || *secondPublish.PublishedRevision != 2 {
		t.Fatalf("new idempotency key must create next publication version: result=%+v err=%v", secondPublish, err)
	}

	versions, err := service.Versions(ctx, principal, model.Model.ID)
	if err != nil || len(versions) != 2 || versions[0].Snapshot.Revision != 1 || versions[0].Changelog != "first stable version" || versions[1].Snapshot.Revision != 2 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	version, err := service.Version(ctx, principal, model.Model.ID, 1)
	if err != nil || version.Snapshot.Name != "Pump v2" {
		t.Fatalf("version=%+v err=%v", version, err)
	}
	list, total, err := service.List(ctx, principal, 1, 10, "Pump")
	if err != nil || total != 1 || len(list) != 1 || list[0].Model.Name != "Pump v2" {
		t.Fatalf("list=%+v total=%d err=%v", list, total, err)
	}
}

func TestTenantIsolationAndAuthorization(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	owner := writerPrincipal("tenant-a", "owner")
	created, err := service.Create(ctx, owner, "tenant-create", core.Model{Name: "Private"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Get(ctx, writerPrincipal("tenant-b", "reader"), core.ModelID(created.ModelID)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read must be hidden as not found, got %v", err)
	}
	if _, err := service.Update(ctx, writerPrincipal("tenant-b", "writer"), "tenant-update", core.ModelID(created.ModelID), 1, core.Model{Name: "stolen"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant update must not find target, got %v", err)
	}
	noCapability := core.Principal{SubjectID: "reader", TenantID: "tenant-a", Capabilities: []string{string(core.CapabilityRead)}}
	if _, err := service.Update(ctx, noCapability, "denied-update", core.ModelID(created.ModelID), 1, core.Model{Name: "denied"}); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("unauthorized update should fail before mutation, got %v", err)
	}
	if len(repo.audits) != 1 || len(repo.events) != 1 {
		t.Fatalf("denied/cross-tenant requests wrote data: audits=%d events=%d", len(repo.audits), len(repo.events))
	}
}

func TestIdempotencyReplayPayloadConflictAndCAS(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	principal := writerPrincipal("tenant-a", "user-a")

	first, err := service.Create(ctx, principal, "create-same", core.Model{Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Create(ctx, principal, "create-same", core.Model{Name: "First", ID: "ignored", TenantID: "ignored"})
	if err != nil || !replay.Replayed || replay.ModelID != first.ModelID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if _, err := service.Create(ctx, principal, "create-same", core.Model{Name: "Different"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same key with different payload should conflict, got %v", err)
	}
	firstPublish, err := service.Publish(ctx, principal, "publish-same", core.ModelID(first.ModelID), 1, "first")
	if err != nil || firstPublish.PublishedRevision == nil || *firstPublish.PublishedRevision != 1 {
		t.Fatalf("initial publication=%+v err=%v", firstPublish, err)
	}
	publishReplay, err := service.Publish(ctx, principal, "publish-same", core.ModelID(first.ModelID), 1, "first")
	if err != nil || !publishReplay.Replayed || *publishReplay.PublishedRevision != 1 {
		t.Fatalf("publish replay=%+v err=%v", publishReplay, err)
	}
	if _, err := service.Publish(ctx, principal, "publish-same", core.ModelID(first.ModelID), 1, "different changelog"); !errors.Is(err, ErrConflict) {
		t.Fatalf("publish payload hash must include changelog, got %v", err)
	}

	record, _ := service.Get(ctx, principal, core.ModelID(first.ModelID))
	replacement := record.Model
	replacement.Name = "Changed"
	if _, err := service.Update(ctx, principal, "cas-update", record.Model.ID, 99, replacement); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale expected revision should conflict, got %v", err)
	}
	if got, _ := service.Get(ctx, principal, record.Model.ID); got.Model.DraftRevision != 1 || got.Model.Name != "First" {
		t.Fatalf("stale CAS changed model: %+v", got)
	}
}

func TestIdempotencyHashNormalizesFieldOrderAndIgnoresServerFields(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	principal := writerPrincipal("tenant-a", "user-a")
	firstFields := []core.Field{telemetryField("temperature", 2), telemetryField("switch", 1)}
	created, err := service.Create(ctx, principal, "normalized-create", core.Model{Name: "Normalized", Fields: firstFields})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.Create(ctx, principal, "normalized-create", core.Model{
		ID: "attacker-id", TenantID: "attacker-tenant", DraftRevision: 999, PublishedRevision: 888,
		Name: "Normalized", Fields: []core.Field{firstFields[1], firstFields[0]},
	})
	if err != nil || !replayed.Replayed || replayed.ModelID != created.ModelID {
		t.Fatalf("reordered semantic create should replay: result=%+v err=%v", replayed, err)
	}

	replacement := core.Model{Name: "Normalized v2", Fields: firstFields}
	updated, err := service.Update(ctx, principal, "normalized-update", core.ModelID(created.ModelID), 1, replacement)
	if err != nil || updated.DraftRevision != 2 {
		t.Fatalf("update=%+v err=%v", updated, err)
	}
	replayedUpdate, err := service.Update(ctx, principal, "normalized-update", core.ModelID(created.ModelID), 1, core.Model{
		ID: "different-client-id", TenantID: "different-client-tenant", DraftRevision: 77, PublishedRevision: 66,
		Name: "Normalized v2", Fields: []core.Field{firstFields[1], firstFields[0]},
	})
	if err != nil || !replayedUpdate.Replayed || replayedUpdate.DraftRevision != 2 {
		t.Fatalf("reordered semantic update should replay: result=%+v err=%v", replayedUpdate, err)
	}
	if _, err := service.Update(ctx, principal, "normalized-stale", core.ModelID(created.ModelID), 1, replacement); !errors.Is(err, ErrConflict) {
		t.Fatalf("semantic payload normalization must not bypass CAS: %v", err)
	}
}

func TestSemanticComparisonIgnoresRevisionAndRevisionOverflowFails(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	principal := writerPrincipal("tenant-a", "user-a")
	created, err := service.Create(ctx, principal, "overflow-create", core.Model{Name: "Stable"})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := service.Get(ctx, principal, core.ModelID(created.ModelID))
	other := model.Model
	other.DraftRevision = 999
	other.PublishedRevision = 123
	if !sameModel(model.Model, other) {
		t.Fatal("semantic equality must ignore draft and published revision counters")
	}

	repo.mu.Lock()
	key := memoryRecordKey(string(principal.TenantID), created.ModelID)
	record := repo.records[key]
	record.Model.DraftRevision = ^uint64(0)
	repo.records[key] = record
	repo.mu.Unlock()
	replacement := record.Model
	replacement.Name = "Overflow"
	if _, err := service.Update(ctx, principal, "overflow-update", record.Model.ID, ^uint64(0), replacement); !errors.Is(err, ErrInvalid) {
		t.Fatalf("revision overflow must fail as invalid, got %v", err)
	}
	got, _ := service.Get(ctx, principal, record.Model.ID)
	if got.Model.Name != "Stable" || got.Model.DraftRevision != ^uint64(0) {
		t.Fatalf("overflow partially changed model: %+v", got)
	}
}

func TestReadMethodsRequireReadCapability(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	principal := writerPrincipal("tenant-a", "writer")
	created, err := service.Create(ctx, principal, "read-auth-create", core.Model{Name: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	denied := core.Principal{SubjectID: "no-read", TenantID: principal.TenantID, Capabilities: []string{string(core.CapabilityWrite)}}
	if _, err := service.Get(ctx, denied, core.ModelID(created.ModelID)); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("Get without read capability: %v", err)
	}
	if _, _, err := service.List(ctx, denied, 1, 10, ""); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("List without read capability: %v", err)
	}
	if _, err := service.Versions(ctx, denied, core.ModelID(created.ModelID)); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("Versions without read capability: %v", err)
	}
	if _, err := service.Version(ctx, denied, core.ModelID(created.ModelID), 1); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("Version without read capability: %v", err)
	}
}

func TestFailureRollsBackModelAuditEventAndReceipt(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	principal := writerPrincipal("tenant-a", "user-a")
	created, err := service.Create(ctx, principal, "create-before", core.Model{Name: "Before"})
	if err != nil {
		t.Fatal(err)
	}
	record, _ := service.Get(ctx, principal, core.ModelID(created.ModelID))
	replacement := record.Model
	replacement.Name = "After"
	repo.failAt = "event"
	if _, err := service.Update(ctx, principal, "update-rollback", record.Model.ID, 1, replacement); err == nil {
		t.Fatal("expected event failure")
	}
	if got, _ := service.Get(ctx, principal, record.Model.ID); got.Model.Name != "Before" || got.Model.DraftRevision != 1 {
		t.Fatalf("failed transaction partially changed model: %+v", got)
	}
	if len(repo.audits) != 1 || len(repo.events) != 1 {
		t.Fatalf("failed transaction partially wrote audit/event: audits=%d events=%d", len(repo.audits), len(repo.events))
	}
	repo.failAt = ""
	if result, err := service.Update(ctx, principal, "update-rollback", record.Model.ID, 1, replacement); err != nil || result.DraftRevision != 2 {
		t.Fatalf("failed receipt must not persist; same key should be retryable: result=%+v err=%v", result, err)
	}
}

func TestInvalidRequestAndArchivedModelDoNotWrite(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	principal := writerPrincipal("tenant-a", "user-a")
	if _, err := service.Create(ctx, principal, "short", core.Model{Name: "invalid key"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short idempotency key should be rejected, got %v", err)
	}
	created, err := service.Create(ctx, principal, "archive-create", core.Model{Name: "Archived"})
	if err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	key := memoryRecordKey("tenant-a", created.ModelID)
	rec := repo.records[key]
	rec.Lifecycle = "ARCHIVED"
	repo.records[key] = rec
	repo.mu.Unlock()
	if _, err := service.Update(ctx, principal, "archive-update", core.ModelID(created.ModelID), 1, core.Model{Name: "Nope"}); !errors.Is(err, ErrArchived) {
		t.Fatalf("archived model should not be editable, got %v", err)
	}
}

type memoryRepository struct {
	mu        sync.Mutex
	records   map[string]Record
	versions  map[string]map[uint64]Publication
	audits    []Audit
	events    []Event
	receipts  map[string]receipt
	resources map[string]ResourcesRecord
	failAt    string
	activeTx  *memoryTransaction
}

type receipt struct {
	hash   string
	result Result
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{records: map[string]Record{}, versions: map[string]map[uint64]Publication{}, receipts: map[string]receipt{}, resources: map[string]ResourcesRecord{}}
}

func (r *memoryRepository) Execute(ctx context.Context, scope ReceiptScope, fn func(Transaction) (Result, error)) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := receiptKey(scope)
	if prior, ok := r.receipts[key]; ok {
		if prior.hash != scope.PayloadHash {
			return Result{}, ErrConflict
		}
		out := prior.result
		out.Replayed = true
		return out, nil
	}
	copyRecords := make(map[string]Record, len(r.records))
	for key, value := range r.records {
		copyRecords[key] = cloneRecord(value)
	}
	copyVersions := cloneVersions(r.versions)
	copyResources := cloneResourcesMap(r.resources)
	copyAudits := append([]Audit(nil), r.audits...)
	copyEvents := append([]Event(nil), r.events...)
	tx := &memoryTransaction{repo: r, records: copyRecords, versions: copyVersions, resources: copyResources, audits: copyAudits, events: copyEvents}
	r.activeTx = tx
	result, err := fn(tx)
	r.activeTx = nil
	if err != nil {
		return Result{}, err
	}
	r.records, r.versions, r.resources, r.audits, r.events = tx.records, tx.versions, tx.resources, tx.audits, tx.events
	r.receipts[key] = receipt{hash: scope.PayloadHash, result: result}
	return result, nil
}

func (r *memoryRepository) Resources(ctx context.Context, tenant core.TenantID, id core.ModelID) (ResourcesRecord, error) {
	if err := ctx.Err(); err != nil {
		return ResourcesRecord{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[memoryRecordKey(string(tenant), string(id))]; !ok {
		return ResourcesRecord{}, ErrNotFound
	}
	return getMemoryResources(r.resources, tenant, id), nil
}

func (r *memoryRepository) Get(ctx context.Context, tenant core.TenantID, id core.ModelID) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.records[memoryRecordKey(string(tenant), string(id))]
	if !ok {
		return Record{}, ErrNotFound
	}
	return cloneRecord(value), nil
}

func (r *memoryRepository) List(ctx context.Context, tenant core.TenantID, limit, offset int, keyword string) ([]Record, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []Record
	for _, record := range r.records {
		if record.Model.TenantID == tenant && (keyword == "" || stringsContainsFold(record.Model.Name, keyword)) {
			all = append(all, cloneRecord(record))
		}
	}
	sortRecords(all)
	total := len(all)
	if offset >= total {
		return []Record{}, total, nil
	}
	all = all[offset:]
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

func (r *memoryRepository) Versions(ctx context.Context, tenant core.TenantID, id core.ModelID) ([]Publication, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.activeTx != nil {
		return listMemoryVersions(r.activeTx.versions, tenant, id), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return listMemoryVersions(r.versions, tenant, id), nil
}

func listMemoryVersions(source map[string]map[uint64]Publication, tenant core.TenantID, id core.ModelID) []Publication {
	values := source[memoryRecordKey(string(tenant), string(id))]
	out := make([]Publication, 0, len(values))
	for _, value := range values {
		out = append(out, clonePublication(value))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Snapshot.Revision < out[j].Snapshot.Revision })
	return out
}

func (r *memoryRepository) Version(ctx context.Context, tenant core.TenantID, id core.ModelID, revision uint64) (Publication, error) {
	if err := ctx.Err(); err != nil {
		return Publication{}, err
	}
	source := r.versions
	if r.activeTx != nil {
		source = r.activeTx.versions
	} else {
		r.mu.Lock()
		defer r.mu.Unlock()
	}
	value, ok := source[memoryRecordKey(string(tenant), string(id))][revision]
	if !ok {
		return Publication{}, ErrNotFound
	}
	return clonePublication(value), nil
}

type memoryTransaction struct {
	repo      *memoryRepository
	records   map[string]Record
	versions  map[string]map[uint64]Publication
	resources map[string]ResourcesRecord
	audits    []Audit
	events    []Event
}

func (tx *memoryTransaction) Get(ctx context.Context, tenant core.TenantID, id core.ModelID) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	record, ok := tx.records[memoryRecordKey(string(tenant), string(id))]
	if !ok {
		return Record{}, ErrNotFound
	}
	return cloneRecord(record), nil
}

func (tx *memoryTransaction) Insert(ctx context.Context, record Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := memoryRecordKey(string(record.Model.TenantID), string(record.Model.ID))
	if _, exists := tx.records[key]; exists {
		return ErrConflict
	}
	tx.records[key] = cloneRecord(record)
	tx.resources[key] = ResourcesRecord{Document: emptyResources(), Revision: 0}
	return nil
}

func (tx *memoryTransaction) GetResources(ctx context.Context, tenant core.TenantID, id core.ModelID, lock bool) (ResourcesRecord, error) {
	if err := ctx.Err(); err != nil {
		return ResourcesRecord{}, err
	}
	if _, ok := tx.records[memoryRecordKey(string(tenant), string(id))]; !ok {
		return ResourcesRecord{}, ErrNotFound
	}
	return getMemoryResources(tx.resources, tenant, id), nil
}

func (tx *memoryTransaction) SaveResources(ctx context.Context, tenant core.TenantID, id core.ModelID, expected uint64, document ResourcesDocument) (ResourcesRecord, error) {
	if err := ctx.Err(); err != nil {
		return ResourcesRecord{}, err
	}
	key := memoryRecordKey(string(tenant), string(id))
	current, ok := tx.resources[key]
	if !ok || current.Revision != expected {
		return ResourcesRecord{}, ErrConflict
	}
	saved := ResourcesRecord{Document: cloneResources(document), Revision: expected + 1}
	tx.resources[key] = saved
	return saved, nil
}

func (tx *memoryTransaction) Update(ctx context.Context, record Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := memoryRecordKey(string(record.Model.TenantID), string(record.Model.ID))
	if _, exists := tx.records[key]; !exists {
		return ErrNotFound
	}
	tx.records[key] = cloneRecord(record)
	return nil
}

func (tx *memoryTransaction) Publish(ctx context.Context, publication Publication) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := memoryRecordKey(string(publication.Snapshot.TenantID), string(publication.Snapshot.ModelID))
	if tx.versions[key] == nil {
		tx.versions[key] = map[uint64]Publication{}
	}
	if _, exists := tx.versions[key][publication.Snapshot.Revision]; exists {
		return ErrConflict
	}
	tx.versions[key][publication.Snapshot.Revision] = clonePublication(publication)
	record, ok := tx.records[key]
	if !ok {
		return ErrNotFound
	}
	record.Model.PublishedRevision = publication.Snapshot.Revision
	record.Lifecycle = "PUBLISHED"
	tx.records[key] = cloneRecord(record)
	return nil
}

func (tx *memoryTransaction) Audit(ctx context.Context, audit Audit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx.repo.failAt == "audit" {
		return errors.New("injected audit failure")
	}
	tx.audits = append(tx.audits, audit)
	return nil
}

func (tx *memoryTransaction) Emit(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx.repo.failAt == "event" {
		return errors.New("injected event failure")
	}
	tx.events = append(tx.events, event)
	return nil
}

func receiptKey(scope ReceiptScope) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s", scope.TenantID, scope.SubjectID, scope.Operation, scope.Key)
}
func memoryRecordKey(tenant, id string) string { return tenant + "\x00" + id }

func getMemoryResources(source map[string]ResourcesRecord, tenant core.TenantID, id core.ModelID) ResourcesRecord {
	if r, ok := source[memoryRecordKey(string(tenant), string(id))]; ok {
		r.Document = cloneResources(r.Document)
		return r
	}
	return ResourcesRecord{Document: emptyResources(), Revision: 0}
}

func cloneResourcesMap(in map[string]ResourcesRecord) map[string]ResourcesRecord {
	out := make(map[string]ResourcesRecord, len(in))
	for k, v := range in {
		v.Document = cloneResources(v.Document)
		out[k] = v
	}
	return out
}

func cloneVersions(in map[string]map[uint64]Publication) map[string]map[uint64]Publication {
	out := make(map[string]map[uint64]Publication, len(in))
	for key, values := range in {
		out[key] = make(map[uint64]Publication, len(values))
		for revision, value := range values {
			out[key][revision] = clonePublication(value)
		}
	}
	return out
}

func sequenceIDs() IDGenerator {
	var i int
	return func() string { i++; return fmt.Sprintf("id-%08d", i) }
}

func writerPrincipal(tenant, subject string) core.Principal {
	return core.Principal{SubjectID: subject, TenantID: core.TenantID(tenant), Capabilities: []string{string(core.CapabilityRead), string(core.CapabilityWrite), string(core.CapabilityPublish)}}
}

func telemetryField(identifier string, sortOrder int) core.Field {
	return core.Field{Kind: core.FieldTelemetry, Identifier: identifier, Name: core.I18nString{Default: identifier},
		ValueType: &core.ValueType{Kind: core.ScalarFloat}, SortOrder: sortOrder}
}

func stringsContainsFold(value, part string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(part))
}

func sortRecords(records []Record) {
	sort.Slice(records, func(i, j int) bool { return records[i].Model.ID < records[j].Model.ID })
}
