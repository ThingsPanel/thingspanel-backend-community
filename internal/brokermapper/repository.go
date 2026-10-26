package brokermapper

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ErrSnapshotNotFound = errors.New("broker mapper: immutable snapshot not found")
var ErrUnsupportedMapping = errors.New("broker mapper: unsupported mapping")

const (
	DefaultMaxSnapshots  = 256
	DefaultMaxCacheBytes = 32 << 20
)

type Repository struct {
	DB            *gorm.DB
	MaxSnapshots  int
	MaxCacheBytes int

	mu         sync.Mutex
	cache      map[string]*list.Element
	lru        list.List
	cacheBytes int
}

type cacheEntry struct {
	version  string
	snapshot *Snapshot
	bytes    int
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{DB: db, MaxSnapshots: DefaultMaxSnapshots, MaxCacheBytes: DefaultMaxCacheBytes, cache: make(map[string]*list.Element)}
}

// CompileForDeviceConfig freezes all enabled rows after tenant-scoped ownership
// verification. Any unsupported row rejects the complete snapshot.
func (r *Repository) CompileForDeviceConfig(ctx context.Context, tenantID, deviceConfigID string) (*Snapshot, error) {
	if r == nil || r.DB == nil || tenantID == "" || deviceConfigID == "" {
		return nil, errors.New("broker mapper: invalid snapshot scope")
	}
	type mappingRow struct {
		ID             int64   `gorm:"column:id"`
		Direction      string  `gorm:"column:direction"`
		SourceTopic    string  `gorm:"column:source_topic"`
		TargetTopic    string  `gorm:"column:target_topic"`
		Priority       int32   `gorm:"column:priority"`
		DataIdentifier *string `gorm:"column:data_identifier"`
	}
	var rows []mappingRow
	db := r.DB.Session(&gorm.Session{Logger: logger.Discard}).WithContext(ctx)
	err := db.Raw(`SELECT m.id, m.direction, m.source_topic, m.target_topic, m.priority, m.data_identifier
		FROM device_topic_mappings m
		JOIN device_configs c ON c.id = m.device_config_id AND c.tenant_id = ?
		WHERE c.id = ? AND m.enabled = TRUE
		ORDER BY m.priority ASC, m.id ASC`, tenantID, deviceConfigID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	rules := make([]Rule, 0, len(rows))
	for _, row := range rows {
		rule := Rule{ID: row.ID, Direction: row.Direction, SourceTopic: row.SourceTopic, TargetTopic: row.TargetTopic, Priority: row.Priority}
		if row.DataIdentifier != nil {
			rule.DataIdentifier = *row.DataIdentifier
		}
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return Standard(), nil
	}
	snapshot, err := Compile(rules)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedMapping, err)
	}
	body, err := json.Marshal(snapshot.rules)
	if err != nil {
		return nil, err
	}
	err = db.Exec(`INSERT INTO broker_mapping_snapshots (version, rules, created_at)
		VALUES (?, ?::jsonb, now()) ON CONFLICT (version) DO NOTHING`, snapshot.Version, string(body)).Error
	if err != nil {
		return nil, err
	}
	// Read back even after ON CONFLICT: the persisted bytes must hash to the
	// requested version before an owner can grant that version to a client.
	return r.Load(ctx, snapshot.Version)
}

// Load retrieves an already persisted immutable snapshot by its stable version.
// Empty custom mappings use the contract's built-in standard-v1 and need no row.
func (r *Repository) Load(ctx context.Context, version string) (*Snapshot, error) {
	if version == "standard-v1" {
		return Standard(), nil
	}
	if r == nil || r.DB == nil || version == "" {
		return nil, ErrSnapshotNotFound
	}
	if s := r.cached(version); s != nil {
		return s, nil
	}
	var row struct {
		Rules []byte `gorm:"column:rules"`
	}
	err := r.DB.Session(&gorm.Session{Logger: logger.Discard}).WithContext(ctx).
		Raw(`SELECT rules FROM broker_mapping_snapshots WHERE version = ?`, version).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if len(row.Rules) == 0 {
		return nil, ErrSnapshotNotFound
	}
	var rules []Rule
	if err := json.Unmarshal(row.Rules, &rules); err != nil {
		return nil, fmt.Errorf("broker mapper: invalid stored snapshot: %w", err)
	}
	snapshot, err := Compile(rules)
	if err != nil || snapshot.Version != version {
		return nil, errors.New("broker mapper: stored snapshot version mismatch")
	}
	return r.cacheSnapshot(snapshot, len(row.Rules)), nil
}

func (r *Repository) cached(version string) *Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if el := r.cache[version]; el != nil {
		r.lru.MoveToFront(el)
		return el.Value.(*cacheEntry).snapshot
	}
	return nil
}

func (r *Repository) cacheSnapshot(s *Snapshot, size int) *Snapshot {
	maxCount, maxBytes := r.MaxSnapshots, r.MaxCacheBytes
	if maxCount <= 0 {
		maxCount = DefaultMaxSnapshots
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxCacheBytes
	}
	if size > maxBytes {
		return s
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if el := r.cache[s.Version]; el != nil {
		r.lru.MoveToFront(el)
		return el.Value.(*cacheEntry).snapshot
	}
	entry := &cacheEntry{version: s.Version, snapshot: s, bytes: size}
	r.cache[s.Version] = r.lru.PushFront(entry)
	r.cacheBytes += size
	for len(r.cache) > maxCount || r.cacheBytes > maxBytes {
		el := r.lru.Back()
		if el == nil {
			break
		}
		old := el.Value.(*cacheEntry)
		delete(r.cache, old.version)
		r.cacheBytes -= old.bytes
		r.lru.Remove(el)
	}
	return s
}
