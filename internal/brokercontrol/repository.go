package brokercontrol

import (
	"context"
	"time"

	"project/internal/brokermapper"
)

type DeviceRecord struct {
	ID                string  `gorm:"column:id"`
	TenantID          string  `gorm:"column:tenant_id"`
	DeviceNumber      string  `gorm:"column:device_number"`
	DeviceNumberCount int64   `gorm:"column:device_number_count"`
	IsEnabled         string  `gorm:"column:is_enabled"`
	ActivateFlag      string  `gorm:"column:activate_flag"`
	ParentID          *string `gorm:"column:parent_id"`
	DeviceConfigID    *string `gorm:"column:device_config_id"`
	DeviceType        string  `gorm:"column:device_type"`
	ServiceAccessID   *string `gorm:"column:service_access_id"`
	AccessWay         *string `gorm:"column:access_way"`
	ProtocolType      *string `gorm:"column:protocol_type"`
}

type ClientIDBinding struct {
	BrokerCell   string    `gorm:"column:broker_cell;primaryKey;size:128"`
	WireClientID string    `gorm:"column:wire_client_id;primaryKey;size:256"`
	TenantID     string    `gorm:"column:tenant_id;not null;size:36"`
	DeviceID     string    `gorm:"column:device_id;not null;size:36"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
}

func (ClientIDBinding) TableName() string { return "broker_client_ids" }

type TransportBinding struct {
	TenantID   string    `gorm:"column:tenant_id;primaryKey;size:36"`
	DeviceID   string    `gorm:"column:device_id;primaryKey;size:36"`
	BrokerCell string    `gorm:"column:broker_cell;not null;size:128"`
	RouteEpoch uint64    `gorm:"column:route_epoch;not null"`
	Phase      string    `gorm:"column:phase;not null;size:24"`
	UpdatedAt  time.Time `gorm:"column:updated_at;not null"`
}

func (TransportBinding) TableName() string { return "broker_transport_bindings" }

// Repository isolates PostgreSQL-specific identity matching and row-locking
// from policy construction, which can be unit tested with deterministic fakes.
type Repository interface {
	FindCredentialMatches(context.Context, string) ([]DeviceRecord, error)
	ResolveServiceTargets(context.Context, []string) ([]DeviceRecord, error)
	ListEnabledChildren(context.Context, string, string) ([]DeviceRecord, error)
	HasEnabledTopicMappings(context.Context, string, string) (bool, error)
	CompileMappingSnapshot(context.Context, string, string) (*brokermapper.Snapshot, error)
	ClaimClientID(context.Context, string, string, string, string) (bool, error)
	ResolveActiveBinding(context.Context, string, string, string) (TransportBinding, error)
}
