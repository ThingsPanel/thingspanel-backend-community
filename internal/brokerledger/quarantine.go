package brokerledger

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	QuarantineBodyLimit            = 2 << 20
	QuarantineTopicLimit           = 65535
	QuarantineReasonLimit          = 256
	QuarantineTenantCountLimit     = int64(1000)
	QuarantineTenantBodyBytesLimit = int64(64 << 20)
	quarantineRetention            = 7 * 24 * time.Hour
)

var (
	ErrQuarantineInvalid  = errors.New("brokerledger: invalid quarantine rejection")
	ErrQuarantineCapacity = errors.New("brokerledger: quarantine tenant capacity exceeded")
	ErrQuarantineFenced   = errors.New("brokerledger: quarantine route is fenced")
	ErrQuarantineConflict = errors.New("brokerledger: quarantine digest collision")
)

// Rejection contains only the trusted scope and opaque original message data.
// It deliberately has no username, password, token, or auth metadata fields.
type Rejection struct {
	TenantID   string
	DeviceID   string
	BrokerCell string
	RouteEpoch uint64
	Topic      string
	Body       []byte
	Reason     string
}

type quarantineRow struct {
	ID          string    `gorm:"column:id;primaryKey"`
	TenantID    string    `gorm:"column:tenant_id"`
	DeviceID    string    `gorm:"column:device_id"`
	BrokerCell  string    `gorm:"column:broker_cell"`
	RouteEpoch  int64     `gorm:"column:route_epoch"`
	Topic       string    `gorm:"column:topic"`
	Body        []byte    `gorm:"column:body"`
	Reason      string    `gorm:"column:reason"`
	Occurrences int64     `gorm:"column:occurrences"`
	FirstSeen   time.Time `gorm:"column:first_seen"`
	LastSeen    time.Time `gorm:"column:last_seen"`
	ExpiresAt   time.Time `gorm:"column:expires_at"`
}

func (quarantineRow) TableName() string { return "broker_ingress_quarantine" }

// RecordRejection commits a trusted rejection into bounded quarantine storage.
// A nil error is the only condition under which the caller may ACK it.
func (s *Store) RecordRejection(ctx context.Context, rejection Rejection) (id string, duplicate bool, err error) {
	if s == nil || s.db == nil {
		return "", false, errors.New("brokerledger: database is nil")
	}
	if ctx == nil {
		return "", false, errors.New("brokerledger: context is nil")
	}
	if err := validateRejection(rejection); err != nil {
		return "", false, err
	}
	id = quarantineID(rejection)
	now := time.Now().UTC()
	row := quarantineRow{
		ID: id, TenantID: rejection.TenantID, DeviceID: rejection.DeviceID,
		BrokerCell: rejection.BrokerCell, RouteEpoch: int64(rejection.RouteEpoch),
		Topic: rejection.Topic, Body: append([]byte(nil), rejection.Body...), Reason: rejection.Reason,
		Occurrences: 1, FirstSeen: now, LastSeen: now, ExpiresAt: now.Add(quarantineRetention),
	}
	quietDB := s.db.Session(&gorm.Session{Logger: logger.Discard})
	err = quietDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize quota checks across all devices in one tenant.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('brokerquarantine'),hashtext(?))`, rejection.TenantID).Error; err != nil {
			return err
		}
		var binding struct {
			BrokerCell string
			RouteEpoch int64
			Phase      string
		}
		br := tx.Raw(`SELECT broker_cell,route_epoch,phase FROM broker_transport_bindings WHERE tenant_id=? AND device_id=? FOR UPDATE`, rejection.TenantID, rejection.DeviceID).Scan(&binding)
		if br.Error != nil {
			return br.Error
		}
		if br.RowsAffected != 1 {
			return ErrQuarantineFenced
		}
		if rejection.Reason == "retired_transport_route" {
			retired, e := auditedRouteRetired(tx, rejection.TenantID, rejection.DeviceID, rejection.BrokerCell, int64(rejection.RouteEpoch), bindingRow{BrokerCell: binding.BrokerCell, RouteEpoch: binding.RouteEpoch, Phase: binding.Phase})
			if e != nil {
				return e
			}
			if !retired {
				return ErrQuarantineFenced
			}
		} else if binding.BrokerCell != rejection.BrokerCell || binding.RouteEpoch != int64(rejection.RouteEpoch) || binding.Phase != "active" {
			return ErrQuarantineFenced
		}

		var existing quarantineRow
		lookup := tx.Raw(`SELECT * FROM broker_ingress_quarantine WHERE id=? FOR UPDATE`, id).Scan(&existing)
		if lookup.Error != nil {
			return lookup.Error
		}
		if lookup.RowsAffected == 1 {
			if !sameRejection(existing, row) {
				return ErrQuarantineConflict
			}
			var active bool
			if err := tx.Raw(`SELECT expires_at>now() FROM broker_ingress_quarantine WHERE id=?`, id).Scan(&active).Error; err != nil {
				return err
			}
			if active {
				if err := tx.Exec(`UPDATE broker_ingress_quarantine SET occurrences=occurrences+1,last_seen=now() WHERE id=?`, id).Error; err != nil {
					return err
				}
				duplicate = true
				return nil
			}
			if err := tx.Exec(`UPDATE broker_ingress_quarantine SET occurrences=1,first_seen=?,last_seen=?,expires_at=? WHERE id=?`, row.FirstSeen, row.LastSeen, row.ExpiresAt, id).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}

		var usage struct {
			Count int64 `gorm:"column:count"`
			Bytes int64 `gorm:"column:bytes"`
		}
		if err := tx.Raw(`SELECT count(*) AS count, coalesce(sum(octet_length(body)),0) AS bytes FROM broker_ingress_quarantine WHERE tenant_id=? AND expires_at>now()`, rejection.TenantID).Scan(&usage).Error; err != nil {
			return err
		}
		if usage.Count > s.quarantineCountLimit || usage.Bytes > s.quarantineBodyBytesLimit {
			return ErrQuarantineCapacity
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}
	return id, duplicate, nil
}

func validateRejection(r Rejection) error {
	if r.TenantID == "" || len(r.TenantID) > 36 || r.DeviceID == "" || len(r.DeviceID) > 36 ||
		r.BrokerCell == "" || len(r.BrokerCell) > 128 || r.RouteEpoch == 0 || r.RouteEpoch > math.MaxInt64 ||
		r.Topic == "" || len(r.Topic) > QuarantineTopicLimit || !utf8.ValidString(r.Topic) ||
		strings.ContainsAny(r.Topic, "+#\x00") || len(r.Body) > QuarantineBodyLimit ||
		r.Reason == "" || len(r.Reason) > QuarantineReasonLimit || !utf8.ValidString(r.Reason) {
		return ErrQuarantineInvalid
	}
	for _, s := range []string{r.Topic, r.Reason} {
		for _, c := range s {
			if c < 0x20 || c == 0x7f {
				return ErrQuarantineInvalid
			}
		}
	}
	return nil
}

func quarantineID(r Rejection) string {
	h := sha256.New()
	write := func(data []byte) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(data)))
		_, _ = h.Write(n[:])
		_, _ = h.Write(data)
	}
	write([]byte(r.TenantID))
	write([]byte(r.DeviceID))
	write([]byte(r.BrokerCell))
	var epoch [8]byte
	binary.BigEndian.PutUint64(epoch[:], r.RouteEpoch)
	write(epoch[:])
	write([]byte(r.Topic))
	write(r.Body)
	write([]byte(r.Reason))
	return hex.EncodeToString(h.Sum(nil))
}

func sameRejection(a, b quarantineRow) bool {
	return a.TenantID == b.TenantID && a.DeviceID == b.DeviceID && a.BrokerCell == b.BrokerCell &&
		a.RouteEpoch == b.RouteEpoch && a.Topic == b.Topic && string(a.Body) == string(b.Body) && a.Reason == b.Reason
}
