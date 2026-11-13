package uplink

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"gorm.io/gorm"
	"project/internal/brokerledger"
	"project/internal/brokersemantic"
	"project/internal/model"
	"project/internal/processor"
	"project/internal/uplinkbinding"
)

type permanentUplinkRejection struct{ device string }

func (e permanentUplinkRejection) Error() string   { return "native uplink schema rejected" }
func (e permanentUplinkRejection) Permanent() bool { return true }
func (e permanentUplinkRejection) Unwrap() error   { return brokersemantic.ErrUplinkInvalid }

// WithContracts installs the process-scoped immutable publication reader. Its
// cache holds no mutable device/config bindings.
func (p *ReliableProcessor) WithContracts(c uplinkbinding.ContractReader) *ReliableProcessor {
	p.contracts = c
	return p
}

type preparedLogicalUplink struct {
	device    model.Device
	binding   uplinkbinding.Snapshot
	payload   []byte
	rejection string
}

// Prepare performs script/HTTP work without receipt, route or row write locks.
// Returned work contains only local validation/fences and atomic business writes.
func (p *ReliableProcessor) Prepare(ctx context.Context, db *gorm.DB, msg brokerledger.Message) (*brokerledger.PreparedOperation, error) {
	if strings.HasSuffix(msg.Stream, "response") {
		return &brokerledger.PreparedOperation{Write: p.Process}, nil
	}
	if strings.TrimPrefix(msg.Stream, "gateway_") == "telemetry" {
		return nil, errors.New("telemetry does not use reliable receipt writer")
	}
	batch, err := p.prepareMessages(ctx, db, msg)
	observations := batchObservations(batch, msg.TenantID, msg.DeviceID, msg.ID, strings.TrimPrefix(msg.Stream, "gateway_"))
	observePending(observations)
	if err != nil {
		operation := &brokerledger.PreparedOperation{AfterOutcome: receiptObserver(observations, preparationFailureReason(err))}
		if batch != nil {
			operation.BeforeReject = func(ctx context.Context, tx *gorm.DB, current brokerledger.Message) error {
				return batch.check(ctx, tx)
			}
		}
		return operation, err
	}

	return &brokerledger.PreparedOperation{AfterOutcome: receiptObserver(observations, ""), Write: func(ctx context.Context, tx *gorm.DB, current brokerledger.Message) error {
		if current.ID != msg.ID || current.TenantID != msg.TenantID || current.DeviceID != msg.DeviceID {
			return uplinkbinding.ErrFenced
		}
		if e := batch.check(ctx, tx); e != nil {
			return e
		}

		for _, entry := range batch.entries {
			if e := p.write(ctx, tx, &entry.device, current, batch.family, entry.payload); e != nil {
				return e
			}
		}
		return nil
	}}, nil
}

type preparedUplinkBatch struct {
	root     string
	family   string
	bindings map[string]uplinkbinding.Snapshot
	ids      []string
	entries  []preparedLogicalUplink
	rejected []preparedLogicalUplink
}

func (p *ReliableProcessor) prepareMessages(ctx context.Context, db *gorm.DB, msg brokerledger.Message, atomic ...bool) (*preparedUplinkBatch, error) {
	bestEffort := len(atomic) > 0 && !atomic[0]
	family := strings.TrimPrefix(msg.Stream, "gateway_")
	if family != "event" && family != "attribute" && family != "telemetry" {
		return nil, errors.New("unsupported reliable business stream")
	}
	root, err := uplinkbinding.Read(ctx, db, msg.TenantID, msg.DeviceID)
	if err != nil {
		return nil, err
	}
	data := msg.Payload
	if root.ConfigID() != "" {
		out, e := p.decoder.Decode(ctx, &processor.DecodeInput{DeviceConfigID: root.ConfigID(), Type: processor.DataType(family), RawData: data, Timestamp: messageTime(msg).UnixMilli()})
		if e != nil {
			return nil, e
		}
		if out == nil || !out.Success {
			return nil, errors.New("reliable ingress decoder failed")
		}
		data = out.Data
	}
	bindings := map[string]uplinkbinding.Snapshot{root.DeviceID(): root}
	var entries []preparedLogicalUplink
	count := 0
	var walk func(string, []byte, bool, int, string, string) error
	walk = func(id string, raw []byte, gateway bool, depth int, expectedParent, expectedAddress string) error {
		if depth > 8 || count >= 1000 {
			return errors.New("gateway ingress expansion limit")
		}
		count++
		s, ok := bindings[id]
		if !ok {
			var e error
			s, e = uplinkbinding.Read(ctx, db, msg.TenantID, id)
			if e != nil {
				return e
			}
			bindings[id] = s
		}
		if expectedParent != "" && !s.LinkedTo(expectedParent, expectedAddress) {
			return uplinkbinding.ErrFenced
		}
		var d model.Device
		if e := db.WithContext(ctx).Where("id=? AND tenant_id=? AND is_enabled='enabled'", id, msg.TenantID).Take(&d).Error; e != nil {
			return e
		}
		// The decoder is selected by the authoritative root snapshot. Cached
		// DeviceConfigID values never select a different script in this path.
		if d.DeviceConfigID == nil && s.ConfigID() != "" || d.DeviceConfigID != nil && *d.DeviceConfigID != s.ConfigID() {
			return uplinkbinding.ErrFenced
		}
		appendLogical := func(body []byte) error {
			entries = append(entries, preparedLogicalUplink{device: d, binding: s, payload: append([]byte(nil), body...)})
			return nil
		}

		if !gateway {
			return appendLogical(raw)
		}
		var frame struct {
			Gateway  json.RawMessage            `json:"gateway_data"`
			Devices  map[string]json.RawMessage `json:"sub_device_data"`
			Gateways map[string]json.RawMessage `json:"sub_gateway_data"`
		}
		if e := json.Unmarshal(raw, &frame); e != nil {
			return e
		}
		if len(frame.Gateway) > 0 && string(frame.Gateway) != "null" {
			if e := appendLogical(frame.Gateway); e != nil {
				return e
			}
		}
		for _, g := range []struct {
			m       map[string]json.RawMessage
			gateway bool
		}{{frame.Devices, false}, {frame.Gateways, true}} {
			addresses := make([]string, 0, len(g.m))
			for address := range g.m {
				addresses = append(addresses, address)
			}
			sort.Strings(addresses)
			for _, address := range addresses {
				if count >= 1000 {
					return errors.New("gateway ingress expansion limit")
				}
				var child model.Device
				if e := db.WithContext(ctx).Where("parent_id=? AND tenant_id=? AND sub_device_addr=? AND is_enabled='enabled'", id, msg.TenantID, address).Take(&child).Error; e != nil {
					if bestEffort && errors.Is(e, gorm.ErrRecordNotFound) {
						count++
						if p.logger != nil {
							p.logger.Warn("gateway bus skipped unknown child address")
						}
						continue
					}
					return e
				}
				if e := walk(child.ID, g.m[address], g.gateway, depth+1, id, address); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if err = walk(msg.DeviceID, data, strings.HasPrefix(msg.Stream, "gateway_"), 0, "", ""); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(bindings))
	for id := range bindings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	batch := &preparedUplinkBatch{root: msg.DeviceID, family: family, bindings: bindings, ids: ids, entries: entries}
	for _, id := range ids {
		s := bindings[id]
		if s.Native() {
			if e := brokersemantic.ValidateUplinkJSON(data); e != nil {
				return batch, permanentUplinkRejection{s.DeviceID()}
			}
			break
		}
	}
	var accepted []preparedLogicalUplink
	for _, entry := range entries {
		e := entry.binding.Validate(ctx, p.contracts, family, entry.payload)
		if e == nil && entry.binding.Native() {
			e = validateNativeStorageRepresentation(family, entry.payload)
		}
		if e != nil {
			if bestEffort {
				entry.rejection = preparationFailureReason(e)
				batch.rejected = append(batch.rejected, entry)
				continue
			}
			if errors.Is(e, brokersemantic.ErrUplinkInvalid) {
				return batch, permanentUplinkRejection{entry.device.ID}
			}
			return batch, e
		}
		accepted = append(accepted, entry)
	}
	if bestEffort {
		batch.entries = accepted
	}
	return batch, nil
}

func (b *preparedUplinkBatch) pathBindings(id string) []uplinkbinding.Snapshot {
	var out []uplinkbinding.Snapshot
	for depth := 0; depth <= 8; depth++ {
		s, ok := b.bindings[id]
		if !ok {
			return []uplinkbinding.Snapshot{{}}
		}
		out = append(out, s)
		if id == b.root {
			return out
		}
		id = s.ParentID()
	}
	return []uplinkbinding.Snapshot{{}}
}

func (b *preparedUplinkBatch) check(ctx context.Context, tx *gorm.DB) error {
	snapshots := make([]uplinkbinding.Snapshot, 0, len(b.ids))
	for _, id := range b.ids {
		snapshots = append(snapshots, b.bindings[id])
	}
	return uplinkbinding.CheckAll(ctx, tx, snapshots)
}
