// Package uplinkbinding fences logical-device publication bindings. Immutable
// contracts may be cached; these mutable Community rows must never be cached.
package uplinkbinding

import (
	"context"
	"errors"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"project/internal/brokersemantic"
	"project/internal/model"
	"project/internal/uplinkcontract"
)

var ErrFenced = errors.New("uplink device or model binding changed")

type optionalString struct {
	Value   string
	Present bool
}
type optionalInt struct {
	Value   int
	Present bool
}

func str(p *string) optionalString {
	if p == nil {
		return optionalString{}
	}
	return optionalString{*p, true}
}
func num(p *int) optionalInt {
	if p == nil {
		return optionalInt{}
	}
	return optionalInt{*p, true}
}

// Snapshot contains no caller-editable pins. It also represents an unbound
// device, so a queued legacy message cannot bypass a subsequent native binding.
type Snapshot struct {
	tenant, device             string
	config, parent, address    optionalString
	legacy, model, publication optionalString
	version                    optionalInt
	bindingRevision            int
}

func (s Snapshot) DeviceID() string { return s.device }
func (s Snapshot) TenantID() string { return s.tenant }
func (s Snapshot) ConfigID() string { return s.config.Value }
func (s Snapshot) ParentID() string {return s.parent.Value}
func (s Snapshot) LinkedTo(parent, address string) bool {
	return s.parent.Present && s.address.Present && s.parent.Value == parent && s.address.Value == address
}
func (s Snapshot) Native() bool { return s.model.Present || s.publication.Present || s.version.Present }

// Read is called on the ordinary DB handle BEFORE decoding/contract I/O. No
// transaction or row lock crosses the remote contract call. Final writes must
// call Check inside their existing business transaction.
func Read(ctx context.Context, db *gorm.DB, tenant, device string) (Snapshot, error) {
	return read(ctx, db, tenant, device, false)
}

func read(ctx context.Context, db *gorm.DB, tenant, device string, lock bool) (Snapshot, error) {
	var s Snapshot
	if db == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(device) == "" {
		return s, ErrFenced
	}
	q := db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "SHARE"})
	}
	var d model.Device
	if err := q.Where("id=? AND tenant_id=? AND is_enabled='enabled'", device, tenant).Take(&d).Error; err != nil {
		return s, err
	}
	s = Snapshot{tenant: tenant, device: device, config: str(d.DeviceConfigID), parent: str(d.ParentID), address: str(d.SubDeviceAddr)}
	if s.config.Value == "" {
		return s, nil
	}
	var c model.DeviceConfig
	q = db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "SHARE"})
	}
	if err := q.Where("id=? AND tenant_id=?", s.config.Value, tenant).Take(&c).Error; err != nil {
		return Snapshot{}, err
	}
	s.legacy, s.model, s.publication, s.version, s.bindingRevision = str(c.DeviceTemplateID), str(c.NativeThingModelID), str(c.NativeThingModelSnapshotID), num(c.NativeThingModelRevision), c.NativeBindingRevision
	if s.Native() && (!s.model.Present || s.model.Value == "" || !s.publication.Present || s.publication.Value == "" || !s.version.Present || s.version.Value < 1 || s.bindingRevision < 1 || s.legacy.Present) {
		return Snapshot{}, ErrFenced
	}
	return s, nil
}

type ContractReader interface {
	Contract(context.Context, uplinkcontract.Pins) (*brokersemantic.UplinkContract, error)
}

// Validate checks the whole decoded logical message. Absence of a native
// binding intentionally retains the existing legacy payload contract.
func (s Snapshot) Validate(ctx context.Context, reader ContractReader, family string, payload []byte) error {
	if s.tenant == "" || s.device == "" {
		return ErrFenced
	}
	if !s.Native() {
		return nil
	}
	if reader == nil {
		return uplinkcontract.ErrUnavailable
	}
	c, err := reader.Contract(ctx, uplinkcontract.Pins{TenantID: s.tenant, ModelID: s.model.Value, SnapshotID: s.publication.Value, Revision: uint64(s.version.Value)})
	if err != nil {
		return err
	}
	return c.Validate(family, payload)
}

// Check acquires SHARE locks for the existing write transaction, then compares
// every pin including unbound/config/parent identity. It performs no HTTP. A
// caller must not continue writes after any error or invoke this on bare DB.
func (s Snapshot) Check(ctx context.Context, tx *gorm.DB) error {
	return CheckAll(ctx, tx, []Snapshot{s})
}

// CheckAll takes all logical-device locks before all config locks, in stable
// tenant/id order across the complete batch. No per-item lock interleaving.
func CheckAll(ctx context.Context, tx *gorm.DB, snapshots []Snapshot) error {
	if tx == nil {
		return ErrFenced
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return ErrFenced
	}
	if len(snapshots) == 0 {
		return ErrFenced
	}
	expected := map[string]Snapshot{}
	pairs := [][]any{}
	for _, s := range snapshots {
		if s.tenant == "" || s.device == "" {
			return ErrFenced
		}
		key := s.tenant + "\x00" + s.device
		if old, found := expected[key]; found {
			if old != s {
				return ErrFenced
			}
			continue
		}
		expected[key] = s
		pairs = append(pairs, []any{s.tenant, s.device})
	}
	sort.Slice(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		return a[0].(string) < b[0].(string) || a[0] == b[0] && a[1].(string) < b[1].(string)
	})
	var devices []model.Device
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("(tenant_id,id) IN ? AND is_enabled='enabled'", pairs).Order("tenant_id,id").Find(&devices).Error; err != nil {
		return err
	}
	if len(devices) != len(expected) {
		return ErrFenced
	}
	configs := map[string]model.DeviceConfig{}
	configPairs := [][]any{}
	seen := map[string]bool{}
	for _, d := range devices {
		s := expected[d.TenantID+"\x00"+d.ID]
		if str(d.DeviceConfigID) != s.config || str(d.ParentID) != s.parent || str(d.SubDeviceAddr) != s.address {
			return ErrFenced
		}
		if s.config.Value != "" {
			key := s.tenant + "\x00" + s.config.Value
			if !seen[key] {
				seen[key] = true
				configPairs = append(configPairs, []any{s.tenant, s.config.Value})
			}
		}
	}
	if len(configPairs) > 0 {
		sort.Slice(configPairs, func(i, j int) bool {
			a, b := configPairs[i], configPairs[j]
			return a[0].(string) < b[0].(string) || a[0] == b[0] && a[1].(string) < b[1].(string)
		})
		var rows []model.DeviceConfig
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("(tenant_id,id) IN ?", configPairs).Order("tenant_id,id").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != len(configPairs) {
			return ErrFenced
		}
		for _, c := range rows {
			configs[c.TenantID+"\x00"+c.ID] = c
		}
	}
	for _, s := range expected {
		if s.config.Value == "" {
			continue
		}
		c := configs[s.tenant+"\x00"+s.config.Value]
		current := s
		current.legacy, current.model, current.publication, current.version, current.bindingRevision = str(c.DeviceTemplateID), str(c.NativeThingModelID), str(c.NativeThingModelSnapshotID), num(c.NativeThingModelRevision), c.NativeBindingRevision
		if current != s {
			return ErrFenced
		}
	}
	return nil
}
