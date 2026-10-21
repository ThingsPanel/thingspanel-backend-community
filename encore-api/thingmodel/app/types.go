// Package app implements model application operations without host, database,
// legacy transport, or device protocol dependencies.
package app

import (
	"context"
	"encoding/json"
	"encore.app/thingmodel/core"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("thingmodel: not found")
	ErrConflict = errors.New("thingmodel: revision or idempotency conflict")
	ErrInvalid  = errors.New("thingmodel: invalid request")
	ErrArchived = errors.New("thingmodel: model is archived")
)

type Record struct {
	Model     core.Model
	Lifecycle string
	UpdatedAt time.Time
}

type Result struct {
	Status            string  `json:"status"`
	Lifecycle         string  `json:"lifecycle"`
	ModelID           string  `json:"modelID"`
	DraftRevision     uint64  `json:"draftRevision"`
	PublishedRevision *uint64 `json:"publishedRevision,omitempty"`
	ConsumerState     string  `json:"consumerState"`
	Checksum          *string `json:"checksum,omitempty"`
	Replayed          bool    `json:"replayed"`
	ResourcesRevision *uint64 `json:"resourcesRevision,omitempty"`
}

type ResourceProfile struct {
	Author      string   `json:"author"`
	Tags        []string `json:"tags"`
	Cover       string   `json:"cover"`
	Version     string   `json:"version"`
	Brand       string   `json:"brand"`
	ModelNumber string   `json:"modelNumber"`
}

// ResourcesDocument contains presentation and catalog resources, outside the
// semantic core model. Nil chart configs represent JSON null.
type ResourcesDocument struct {
	Profile        ResourceProfile `json:"profile"`
	WebChartConfig json.RawMessage `json:"webChartConfig"`
	AppChartConfig json.RawMessage `json:"appChartConfig"`
}

type ResourcesRecord struct {
	Document ResourcesDocument
	Revision uint64
}

type ReceiptScope struct {
	TenantID    core.TenantID
	SubjectID   string
	Operation   string
	Key         string
	PayloadHash string
}

type Publication struct {
	Snapshot          core.Snapshot
	Changelog         string
	PublishedBy       string
	CreatedAt         time.Time
	Resources         ResourcesRecord
	ResourcesChecksum string
}

type Audit struct {
	ID        string
	SubjectID string
	Operation string
	Before    *Record
	After     Record
}

type Event struct {
	ID            string
	Operation     string
	ModelID       core.ModelID
	TenantID      core.TenantID
	DraftRevision uint64
	Snapshot      *core.Snapshot
}

// Transaction serializes model mutations; all writes and the command receipt
// commit together. Get locks the row until the transaction completes.
type Transaction interface {
	Get(context.Context, core.TenantID, core.ModelID) (Record, error)
	Insert(context.Context, Record) error
	Update(context.Context, Record) error
	Publish(context.Context, Publication) error
	GetResources(context.Context, core.TenantID, core.ModelID, bool) (ResourcesRecord, error)
	SaveResources(context.Context, core.TenantID, core.ModelID, uint64, ResourcesDocument) (ResourcesRecord, error)
	Audit(context.Context, Audit) error
	Emit(context.Context, Event) error
}

type Repository interface {
	Execute(context.Context, ReceiptScope, func(Transaction) (Result, error)) (Result, error)
	Get(context.Context, core.TenantID, core.ModelID) (Record, error)
	List(context.Context, core.TenantID, int, int, string) ([]Record, int, error)
	Versions(context.Context, core.TenantID, core.ModelID) ([]Publication, error)
	Version(context.Context, core.TenantID, core.ModelID, uint64) (Publication, error)
	Resources(context.Context, core.TenantID, core.ModelID) (ResourcesRecord, error)
}

type IDGenerator func() string
