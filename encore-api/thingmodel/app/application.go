// Package app implements the application use cases for ThingModel. It depends
// only on the domain package and standard library; hosts and storage adapters
// implement Repository and Transaction.
package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"encore.app/thingmodel/core"
)

// Service coordinates authorization, validation, revisions, audit, events and
// durable idempotency. Repository.Execute must commit its receipt and all
// Transaction writes atomically.
type Service struct {
	repo Repository
	ids  IDGenerator
}

func New(repo Repository, ids IDGenerator) *Service {
	if ids == nil {
		ids = randomID
	}
	return &Service{repo: repo, ids: ids}
}

func randomID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("thingmodel: secure ID generation failed: " + err.Error())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func (s *Service) Create(ctx context.Context, principal core.Principal, key string, model core.Model) (Result, error) {
	return s.CreateWithResources(ctx, principal, key, model, nil)
}

func (s *Service) CreateWithResources(ctx context.Context, principal core.Principal, key string, model core.Model, resources *ResourcesDocument) (Result, error) {
	if err := authorize(principal, principal.TenantID, core.CapabilityWrite); err != nil {
		return Result{}, err
	}
	if err := validateKey(key); err != nil {
		return Result{}, err
	}
	if resources != nil {
		normalized := normalizeResources(*resources)
		resources = &normalized
	}
	model.ID = ""
	model.TenantID = ""
	model.DraftRevision = 0
	model.PublishedRevision = 0
	semantic, err := semanticModel(model)
	if err != nil {
		return Result{}, fmt.Errorf("%w: cannot normalize model payload", ErrInvalid)
	}
	payloadHash, err := hashPayload(struct {
		Model json.RawMessage `json:"model"`
	}{Model: semantic})
	if err != nil {
		return Result{}, err
	}
	if resources != nil {
		if err := validateResources(*resources); err != nil {
			return Result{}, err
		}
		payloadHash, err = hashPayload(struct {
			Model     json.RawMessage   `json:"model"`
			Resources ResourcesDocument `json:"resources"`
		}{semantic, *resources})
		if err != nil {
			return Result{}, err
		}
	}
	scope := ReceiptScope{TenantID: principal.TenantID, SubjectID: principal.SubjectID, Operation: "create", Key: key, PayloadHash: payloadHash}
	return s.repo.Execute(ctx, scope, func(tx Transaction) (Result, error) {
		model.ID = core.ModelID(s.ids())
		model.TenantID = principal.TenantID
		model.DraftRevision = 1
		model.PublishedRevision = 0
		if err := validateModel(model); err != nil {
			return Result{}, err
		}
		record := Record{Model: cloneModel(model), Lifecycle: "DRAFT"}
		if err := tx.Insert(ctx, record); err != nil {
			return Result{}, err
		}
		resourceRecord := ResourcesRecord{Document: emptyResources(), Revision: 0}
		if resources != nil {
			resourceRecord, err = tx.SaveResources(ctx, principal.TenantID, model.ID, 0, *resources)
			if err != nil {
				return Result{}, err
			}
		}
		if err := tx.Audit(ctx, Audit{ID: s.ids(), SubjectID: principal.SubjectID, Operation: "created", After: cloneRecord(record)}); err != nil {
			return Result{}, err
		}
		if err := tx.Emit(ctx, Event{ID: s.ids(), Operation: "created", ModelID: model.ID, TenantID: model.TenantID, DraftRevision: model.DraftRevision}); err != nil {
			return Result{}, err
		}
		out := result("SAVED", record)
		out.ResourcesRevision = uint64ptr(resourceRecord.Revision)
		return out, nil
	})
}

func (s *Service) Update(ctx context.Context, principal core.Principal, key string, modelID core.ModelID, expected uint64, replacement core.Model) (Result, error) {
	return s.UpdateWithResources(ctx, principal, key, modelID, expected, replacement, nil, nil)
}

func (s *Service) UpdateWithResources(ctx context.Context, principal core.Principal, key string, modelID core.ModelID, expected uint64, replacement core.Model, resources *ResourcesDocument, expectedResourcesRevision *uint64) (Result, error) {
	if err := authorize(principal, principal.TenantID, core.CapabilityWrite); err != nil {
		return Result{}, err
	}
	if err := validateKey(key); err != nil {
		return Result{}, err
	}
	if resources != nil {
		normalized := normalizeResources(*resources)
		resources = &normalized
	}
	replacement.ID = ""
	replacement.TenantID = ""
	replacement.DraftRevision = 0
	replacement.PublishedRevision = 0
	semantic, err := semanticModel(replacement)
	if err != nil {
		return Result{}, fmt.Errorf("%w: cannot normalize model payload", ErrInvalid)
	}
	payloadHash, err := hashPayload(struct {
		Target   core.ModelID    `json:"target"`
		Expected uint64          `json:"expected"`
		Model    json.RawMessage `json:"model"`
	}{modelID, expected, semantic})
	if err != nil {
		return Result{}, err
	}
	if resources != nil {
		if expectedResourcesRevision == nil {
			return Result{}, fmt.Errorf("%w: expectedResourcesRevision is required with resources", ErrInvalid)
		}
		if err := validateResources(*resources); err != nil {
			return Result{}, err
		}
		payloadHash, err = hashPayload(struct {
			Target            core.ModelID       `json:"target"`
			Expected          uint64             `json:"expected"`
			Model             json.RawMessage    `json:"model"`
			Resources         *ResourcesDocument `json:"resources"`
			ExpectedResources *uint64            `json:"expectedResourcesRevision"`
		}{modelID, expected, semantic, resources, expectedResourcesRevision})
		if err != nil {
			return Result{}, err
		}
	}
	scope := ReceiptScope{TenantID: principal.TenantID, SubjectID: principal.SubjectID, Operation: "update", Key: key, PayloadHash: payloadHash}
	return s.repo.Execute(ctx, scope, func(tx Transaction) (Result, error) {
		before, err := tx.Get(ctx, principal.TenantID, modelID)
		if err != nil {
			return Result{}, err
		}
		if isArchived(before) {
			return Result{}, ErrArchived
		}
		if before.Model.DraftRevision != expected {
			return Result{}, ErrConflict
		}
		resourceRecord, err := tx.GetResources(ctx, principal.TenantID, modelID, false)
		if err != nil {
			return Result{}, err
		}
		if resources != nil && resourceRecord.Revision != *expectedResourcesRevision {
			return Result{}, ErrConflict
		}
		replacement.ID = modelID
		replacement.TenantID = principal.TenantID
		replacement.DraftRevision = before.Model.DraftRevision
		replacement.PublishedRevision = before.Model.PublishedRevision
		if err := validateModel(replacement); err != nil {
			return Result{}, err
		}
		if sameModel(before.Model, replacement) && resources == nil {
			return result("SAVED", before), nil
		}
		if !sameModel(before.Model, replacement) && replacement.DraftRevision == math.MaxUint64 {
			return Result{}, fmt.Errorf("%w: draft revision overflow", ErrInvalid)
		}
		if !sameModel(before.Model, replacement) {
			replacement.DraftRevision++
		}
		after := Record{Model: cloneModel(replacement), Lifecycle: before.Lifecycle}
		if !sameModel(before.Model, replacement) {
			if err := tx.Update(ctx, after); err != nil {
				return Result{}, err
			}
		}
		if resources != nil {
			resourceRecord, err = tx.SaveResources(ctx, principal.TenantID, modelID, *expectedResourcesRevision, *resources)
			if err != nil {
				return Result{}, err
			}
		}
		operation := "updated"
		if sameModel(before.Model, replacement) {
			operation = "resources.updated"
		}
		if err := tx.Audit(ctx, Audit{ID: s.ids(), SubjectID: principal.SubjectID, Operation: operation, Before: ptrRecord(before), After: cloneRecord(after)}); err != nil {
			return Result{}, err
		}
		eventOperation := "updated"
		if sameModel(before.Model, replacement) {
			eventOperation = "resources-updated"
		}
		if err := tx.Emit(ctx, Event{ID: s.ids(), Operation: eventOperation, ModelID: modelID, TenantID: principal.TenantID, DraftRevision: replacement.DraftRevision}); err != nil {
			return Result{}, err
		}
		out := result("SAVED", after)
		out.ResourcesRevision = uint64ptr(resourceRecord.Revision)
		return out, nil
	})
}

func (s *Service) Publish(ctx context.Context, principal core.Principal, key string, modelID core.ModelID, expected uint64, changelog string) (Result, error) {
	return s.PublishWithResources(ctx, principal, key, modelID, expected, changelog, nil)
}

func (s *Service) PublishWithResources(ctx context.Context, principal core.Principal, key string, modelID core.ModelID, expected uint64, changelog string, expectedResourcesRevision *uint64) (Result, error) {
	if err := authorize(principal, principal.TenantID, core.CapabilityPublish); err != nil {
		return Result{}, err
	}
	if err := validateKey(key); err != nil {
		return Result{}, err
	}
	payloadHash, err := hashPayload(struct {
		Target    core.ModelID `json:"target"`
		Expected  uint64       `json:"expected"`
		Changelog string       `json:"changelog"`
	}{modelID, expected, changelog})
	if err != nil {
		return Result{}, err
	}
	if expectedResourcesRevision != nil {
		payloadHash, err = hashPayload(struct {
			Target            core.ModelID `json:"target"`
			Expected          uint64       `json:"expected"`
			Changelog         string       `json:"changelog"`
			ExpectedResources uint64       `json:"expectedResourcesRevision"`
		}{modelID, expected, changelog, *expectedResourcesRevision})
		if err != nil {
			return Result{}, err
		}
	}
	scope := ReceiptScope{TenantID: principal.TenantID, SubjectID: principal.SubjectID, Operation: "publish", Key: key, PayloadHash: payloadHash}
	return s.repo.Execute(ctx, scope, func(tx Transaction) (Result, error) {
		record, err := tx.Get(ctx, principal.TenantID, modelID)
		if err != nil {
			return Result{}, err
		}
		if isArchived(record) {
			return Result{}, ErrArchived
		}
		if record.Model.DraftRevision != expected {
			return Result{}, ErrConflict
		}
		resources, err := tx.GetResources(ctx, principal.TenantID, modelID, false)
		if err != nil {
			return Result{}, err
		}
		if expectedResourcesRevision != nil && resources.Revision != *expectedResourcesRevision {
			return Result{}, ErrConflict
		}
		snapshot, err := core.BuildSnapshot(record.Model, core.SnapshotID(s.ids()))
		if err != nil {
			return Result{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		publication := Publication{Snapshot: snapshot, Changelog: changelog, PublishedBy: principal.SubjectID, Resources: resources}
		publication.ResourcesChecksum, err = ChecksumResources(resources)
		if err != nil {
			return Result{}, err
		}
		if err := tx.Publish(ctx, publication); err != nil {
			return Result{}, err
		}
		after := cloneRecord(record)
		after.Model.PublishedRevision = snapshot.Revision
		after.Lifecycle = "PUBLISHED"
		if err := tx.Audit(ctx, Audit{ID: s.ids(), SubjectID: principal.SubjectID, Operation: "published", Before: ptrRecord(record), After: cloneRecord(after)}); err != nil {
			return Result{}, err
		}
		snapshotCopy := cloneSnapshot(snapshot)
		if err := tx.Emit(ctx, Event{ID: s.ids(), Operation: "published", ModelID: modelID, TenantID: principal.TenantID, DraftRevision: after.Model.DraftRevision, Snapshot: &snapshotCopy}); err != nil {
			return Result{}, err
		}
		out := result("PENDING_CONSUMER", after)
		out.PublishedRevision = uint64ptr(snapshot.Revision)
		out.Checksum = stringptr(snapshot.Checksum)
		out.ResourcesRevision = uint64ptr(resources.Revision)
		// Publication is queued for consumers. Only a consumer may report APPLIED.
		out.ConsumerState = "PENDING"
		return out, nil
	})
}

func (s *Service) Get(ctx context.Context, principal core.Principal, modelID core.ModelID) (Record, error) {
	if err := authorize(principal, principal.TenantID, core.CapabilityRead); err != nil {
		return Record{}, err
	}
	record, err := s.repo.Get(ctx, principal.TenantID, modelID)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

func (s *Service) Resources(ctx context.Context, principal core.Principal, modelID core.ModelID) (ResourcesRecord, error) {
	if err := authorize(principal, principal.TenantID, core.CapabilityRead); err != nil {
		return ResourcesRecord{}, err
	}
	if _, err := s.repo.Get(ctx, principal.TenantID, modelID); err != nil {
		return ResourcesRecord{}, err
	}
	return s.repo.Resources(ctx, principal.TenantID, modelID)
}

func (s *Service) UpdateResources(ctx context.Context, principal core.Principal, key string, modelID core.ModelID, expected uint64, replacement ResourcesDocument) (Result, error) {
	if err := authorize(principal, principal.TenantID, core.CapabilityWrite); err != nil {
		return Result{}, err
	}
	if err := validateKey(key); err != nil {
		return Result{}, err
	}
	replacement = normalizeResources(replacement)
	if err := validateResources(replacement); err != nil {
		return Result{}, err
	}
	payloadHash, err := hashPayload(struct {
		ModelID   core.ModelID      `json:"modelID"`
		Expected  uint64            `json:"expectedRevision"`
		Resources ResourcesDocument `json:"resources"`
	}{modelID, expected, replacement})
	if err != nil {
		return Result{}, err
	}
	scope := ReceiptScope{TenantID: principal.TenantID, SubjectID: principal.SubjectID, Operation: "update-resources", Key: key, PayloadHash: payloadHash}
	return s.repo.Execute(ctx, scope, func(tx Transaction) (Result, error) {
		record, err := tx.Get(ctx, principal.TenantID, modelID)
		if err != nil {
			return Result{}, err
		}
		if isArchived(record) {
			return Result{}, ErrArchived
		}
		current, err := tx.GetResources(ctx, principal.TenantID, modelID, false)
		if err != nil {
			return Result{}, err
		}
		if current.Revision != expected {
			return Result{}, ErrConflict
		}
		saved, err := tx.SaveResources(ctx, principal.TenantID, modelID, expected, replacement)
		if err != nil {
			return Result{}, err
		}
		if err := tx.Audit(ctx, Audit{ID: s.ids(), SubjectID: principal.SubjectID, Operation: "resources.updated", Before: ptrRecord(record), After: cloneRecord(record)}); err != nil {
			return Result{}, err
		}
		if err := tx.Emit(ctx, Event{ID: s.ids(), Operation: "resources-updated", ModelID: modelID, TenantID: principal.TenantID, DraftRevision: record.Model.DraftRevision}); err != nil {
			return Result{}, err
		}
		out := result("SAVED", record)
		out.ResourcesRevision = uint64ptr(saved.Revision)
		return out, nil
	})
}

func (s *Service) List(ctx context.Context, principal core.Principal, page, pageSize int, query string) ([]Record, int, error) {
	if err := authorize(principal, principal.TenantID, core.CapabilityRead); err != nil {
		return nil, 0, err
	}
	if page < 1 || pageSize < 1 || page > int(^uint(0)>>1)/pageSize {
		return nil, 0, ErrInvalid
	}
	offset := (page - 1) * pageSize
	items, total, err := s.repo.List(ctx, principal.TenantID, pageSize, offset, strings.TrimSpace(query))
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i] = cloneRecord(items[i])
	}
	return items, total, nil
}

func (s *Service) Versions(ctx context.Context, principal core.Principal, modelID core.ModelID) ([]Publication, error) {
	if err := authorize(principal, principal.TenantID, core.CapabilityRead); err != nil {
		return nil, err
	}
	if _, err := s.repo.Get(ctx, principal.TenantID, modelID); err != nil {
		return nil, err
	}
	versions, err := s.repo.Versions(ctx, principal.TenantID, modelID)
	if err != nil {
		return nil, err
	}
	for i := range versions {
		versions[i] = clonePublication(versions[i])
	}
	return versions, nil
}

func (s *Service) Version(ctx context.Context, principal core.Principal, modelID core.ModelID, revision uint64) (Publication, error) {
	if err := authorize(principal, principal.TenantID, core.CapabilityRead); err != nil {
		return Publication{}, err
	}
	if _, err := s.repo.Get(ctx, principal.TenantID, modelID); err != nil {
		return Publication{}, err
	}
	publication, err := s.repo.Version(ctx, principal.TenantID, modelID, revision)
	if err != nil {
		return Publication{}, err
	}
	return clonePublication(publication), nil
}

func authorize(principal core.Principal, tenant core.TenantID, capability core.Capability) error {
	return core.Authorize(principal, tenant, capability)
}

func validateKey(key string) error {
	if len(key) < 8 || len(key) > 128 {
		return fmt.Errorf("%w: idempotency key must contain 8 to 128 bytes", ErrInvalid)
	}
	return nil
}

func validateModel(model core.Model) error {
	if issues := core.ValidateModel(model); len(issues) > 0 {
		return fmt.Errorf("%w: %s: %s", ErrInvalid, issues[0].Code, issues[0].Message)
	}
	return nil
}

func emptyResources() ResourcesDocument {
	return ResourcesDocument{Profile: ResourceProfile{Tags: []string{}}}
}

func validateResources(r ResourcesDocument) error {
	p := r.Profile
	if len(p.Author) > 128 || len(p.Cover) > 1024 || len(p.Version) > 64 || len(p.Brand) > 64 || len(p.ModelNumber) > 128 || len(p.Tags) > 64 {
		return fmt.Errorf("%w: resource profile exceeds limits", ErrInvalid)
	}
	for _, tag := range p.Tags {
		if len(tag) > 64 {
			return fmt.Errorf("%w: resource tag exceeds 64 bytes", ErrInvalid)
		}
	}
	for _, raw := range []json.RawMessage{r.WebChartConfig, r.AppChartConfig} {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		if len(trimmed) > 256*1024 || !json.Valid(trimmed) {
			return fmt.Errorf("%w: chart config is invalid or too large", ErrInvalid)
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(trimmed, &object) != nil || object == nil {
			return fmt.Errorf("%w: chart config must be a JSON object or null", ErrInvalid)
		}
	}
	return nil
}

func ChecksumResources(r ResourcesRecord) (string, error) {
	r.Document = normalizeResources(r.Document)
	b, err := json.Marshal(struct {
		Revision uint64            `json:"revision"`
		Document ResourcesDocument `json:"resources"`
	}{r.Revision, r.Document})
	if err != nil {
		return "", ErrInvalid
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func normalizeResources(r ResourcesDocument) ResourcesDocument {
	r = cloneResources(r)
	if r.Profile.Tags == nil {
		r.Profile.Tags = []string{}
	}
	if len(bytes.TrimSpace(r.WebChartConfig)) == 0 || bytes.Equal(bytes.TrimSpace(r.WebChartConfig), []byte("null")) {
		r.WebChartConfig = nil
	}
	if len(bytes.TrimSpace(r.AppChartConfig)) == 0 || bytes.Equal(bytes.TrimSpace(r.AppChartConfig), []byte("null")) {
		r.AppChartConfig = nil
	}
	return r
}

func hashPayload(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: cannot encode idempotency payload", ErrInvalid)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func sameModel(a, b core.Model) bool {
	left, errA := semanticModel(a)
	right, errB := semanticModel(b)
	return errA == nil && errB == nil && string(left) == string(right)
}

func semanticModel(model core.Model) ([]byte, error) {
	fields := append([]core.Field(nil), model.Fields...)
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].SortOrder != fields[j].SortOrder {
			return fields[i].SortOrder < fields[j].SortOrder
		}
		if fields[i].Kind != fields[j].Kind {
			return fields[i].Kind < fields[j].Kind
		}
		return fields[i].Identifier < fields[j].Identifier
	})
	return json.Marshal(struct {
		Name        string           `json:"name"`
		Description *core.I18nString `json:"description,omitempty"`
		Category    *string          `json:"category,omitempty"`
		Fields      []core.Field     `json:"fields"`
	}{model.Name, model.Description, model.Category, fields})
}

func isArchived(record Record) bool {
	return strings.EqualFold(strings.TrimSpace(record.Lifecycle), "ARCHIVED")
}

func result(status string, record Record) Result {
	var publishedRevision *uint64
	if record.Model.PublishedRevision > 0 {
		publishedRevision = uint64ptr(record.Model.PublishedRevision)
	}
	return Result{Status: status, Lifecycle: record.Lifecycle, ModelID: string(record.Model.ID), DraftRevision: record.Model.DraftRevision,
		PublishedRevision: publishedRevision, ConsumerState: "NOT_REQUESTED"}
}

func cloneModel(model core.Model) core.Model {
	data, err := json.Marshal(model)
	if err != nil {
		return model
	}
	var cloned core.Model
	if json.Unmarshal(data, &cloned) != nil {
		return model
	}
	return cloned
}

func cloneRecord(record Record) Record {
	return Record{Model: cloneModel(record.Model), Lifecycle: record.Lifecycle, UpdatedAt: record.UpdatedAt}
}

func ptrRecord(record Record) *Record { copy := cloneRecord(record); return &copy }

func cloneSnapshot(snapshot core.Snapshot) core.Snapshot {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return snapshot
	}
	var cloned core.Snapshot
	if json.Unmarshal(data, &cloned) != nil {
		return snapshot
	}
	cloned.Content = append([]byte(nil), snapshot.Content...)
	return cloned
}

func clonePublication(publication Publication) Publication {
	publication.Snapshot = cloneSnapshot(publication.Snapshot)
	publication.Resources.Document = cloneResources(publication.Resources.Document)
	return publication
}

func cloneResources(r ResourcesDocument) ResourcesDocument {
	if r.Profile.Tags != nil {
		r.Profile.Tags = append([]string{}, r.Profile.Tags...)
	}
	if len(r.WebChartConfig) != 0 {
		r.WebChartConfig = append(json.RawMessage(nil), r.WebChartConfig...)
	}
	if len(r.AppChartConfig) != 0 {
		r.AppChartConfig = append(json.RawMessage(nil), r.AppChartConfig...)
	}
	return r
}

func uint64ptr(value uint64) *uint64 { copy := value; return &copy }
func stringptr(value string) *string { copy := value; return &copy }
