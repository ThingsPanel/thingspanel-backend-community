package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"io"
	"project/internal/brokermapper"
	"project/internal/model"
	"strings"
	"time"
)

// NativeRuntimeConfiguration is a frozen, tenant-owned access configuration.
// Generation belongs to access configuration, never to administrative lifecycle.
type NativeRuntimeConfiguration struct {
	Generation    int64           `json:"generation"`
	Digest        string          `json:"digest"`
	Configuration json.RawMessage `json:"configuration"`
}

type nativeRuntimeConfigurationBody struct {
	DeviceKind string `json:"deviceKind"`
	Model      *struct {
		ModelID           string `json:"modelId"`
		SnapshotID        string `json:"snapshotId"`
		PublishedRevision int    `json:"publishedRevision"`
	} `json:"model"`
	Access struct {
		Mode                string  `json:"mode"`
		Protocol            string  `json:"protocol"`
		ProviderID          *string `json:"providerId"`
		ServiceInstanceID   *string `json:"serviceInstanceId"`
		ConnectionDirection string  `json:"connectionDirection"`
	} `json:"access"`
	TopicMappings []struct {
		ID             string  `json:"id"`
		Name           string  `json:"name"`
		Direction      string  `json:"direction"`
		SourceTopic    string  `json:"sourceTopic"`
		TargetTopic    string  `json:"targetTopic"`
		Priority       int32   `json:"priority"`
		Enabled        bool    `json:"enabled"`
		DataIdentifier *string `json:"dataIdentifier"`
	} `json:"topicMappings"`
	ProcessingScripts []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Phase      string `json:"phase"`
		Language   string `json:"language"`
		Entrypoint string `json:"entrypoint"`
		Content    string `json:"content"`
		Enabled    bool   `json:"enabled"`
	} `json:"processingScripts"`
}

var nativeRuntimeScriptTypes = map[string]string{
	"telemetry_up": "A", "telemetry_down": "B", "attribute_up": "C",
	"attribute_down": "D", "command_down": "E", "event_up": "F",
}

func validateNativeRuntimeConfiguration(in *NativeRuntimeConfiguration) (*nativeRuntimeConfigurationBody, error) {
	if in == nil || in.Generation < 1 || len(in.Configuration) == 0 || len(in.Configuration) > 4<<20 {
		return nil, ErrNativeRegistrationInvalid
	}
	// Decode with UseNumber to retain numeric precision when canonicalizing JSON.
	var value any
	dec := json.NewDecoder(strings.NewReader(string(in.Configuration)))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, ErrNativeRegistrationInvalid
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, ErrNativeRegistrationInvalid
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, ErrNativeRegistrationInvalid
	}
	sum := sha256.Sum256(canonical)
	if in.Digest != hex.EncodeToString(sum[:]) {
		return nil, fmt.Errorf("%w: runtime configuration digest", ErrNativeRegistrationInvalid)
	}
	var cfg nativeRuntimeConfigurationBody
	if json.Unmarshal(canonical, &cfg) != nil || cfg.DeviceKind != "standalone" || cfg.Access.Mode != "direct" || cfg.Access.Protocol != "mqtt" || cfg.Access.ProviderID != nil || cfg.Access.ServiceInstanceID != nil || cfg.Access.ConnectionDirection != "device_to_platform" {
		return nil, ErrNativeRegistrationInvalid
	}
	if cfg.Model != nil {
		for _, s := range []string{cfg.Model.ModelID, cfg.Model.SnapshotID} {
			id, err := uuid.Parse(s)
			if err != nil || id == uuid.Nil || id.String() != s {
				return nil, ErrNativeRegistrationInvalid
			}
		}
		if cfg.Model.PublishedRevision < 1 {
			return nil, ErrNativeRegistrationInvalid
		}
	}
	stages := map[string]bool{}
	for _, script := range cfg.ProcessingScripts {
		if nativeRuntimeScriptTypes[script.Phase] == "" || script.Language != "lua" || script.Entrypoint != "encodeInp" || strings.TrimSpace(script.Content) == "" {
			return nil, ErrNativeRegistrationInvalid
		}
		if script.Enabled {
			if stages[script.Phase] {
				return nil, ErrNativeRegistrationInvalid
			}
			stages[script.Phase] = true
		}
	}
	for _, mapping := range cfg.TopicMappings {
		if (mapping.Direction != "up" && mapping.Direction != "down") || mapping.SourceTopic == "" || mapping.TargetTopic == "" {
			return nil, ErrNativeRegistrationInvalid
		}
	}
	rules := []brokermapper.Rule{}
	for i, m := range cfg.TopicMappings {
		if m.Enabled {
			r := brokermapper.Rule{ID: int64(i + 1), Direction: m.Direction, SourceTopic: m.SourceTopic, TargetTopic: m.TargetTopic, Priority: m.Priority}
			if m.DataIdentifier != nil {
				r.DataIdentifier = *m.DataIdentifier
			}
			rules = append(rules, r)
		}
	}
	if len(rules) > 0 {
		if _, err := brokermapper.Compile(rules); err != nil {
			return nil, fmt.Errorf("%w: unsupported topic mapping", ErrNativeRegistrationInvalid)
		}
	}
	return &cfg, nil
}

type nativeRuntimeConfigurationRecord struct {
	DeviceID       string `gorm:"primaryKey"`
	Generation     int64
	ContentDigest  string
	DeviceConfigID string
	Pending        bool
}

func (nativeRuntimeConfigurationRecord) TableName() string { return "native_runtime_configurations" }

// Each generation gets an immutable managed projection; existing script caches
// and mapping snapshots therefore never observe an in-place configuration edit.
func projectNativeRuntimeConfiguration(tx *gorm.DB, owner nativeRuntimeRegistration, in *NativeRuntimeConfiguration) error {
	cfg, err := validateNativeRuntimeConfiguration(in)
	if err != nil {
		return err
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("native-runtime-config/"+owner.DeviceID+"/"+fmt.Sprint(in.Generation)+"/"+in.Digest)).String()
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 72311032))", id).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	protocol, conn := "MQTT", "A"
	projection := model.DeviceConfig{ID: id, Name: "native-runtime-" + owner.DeviceID, DeviceType: "1", ProtocolType: &protocol, DeviceConnType: &conn, TenantID: owner.TenantID, CreatedAt: now, UpdatedAt: now}
	if cfg.Model != nil {
		projection.NativeThingModelID = &cfg.Model.ModelID
		projection.NativeThingModelSnapshotID = &cfg.Model.SnapshotID
		projection.NativeThingModelRevision = &cfg.Model.PublishedRevision
		projection.NativeBindingRevision = int(in.Generation)
	}
	if err := tx.Create(&projection).Error; err != nil {
		return err
	}
	for _, m := range cfg.TopicMappings {
		// Disabled rows remain part of the frozen digest but cannot authorize traffic.
		if !m.Enabled {
			continue
		}
		row := model.DeviceTopicMapping{DeviceConfigID: id, Name: m.Name, Direction: m.Direction, SourceTopic: m.SourceTopic, TargetTopic: m.TargetTopic, Priority: m.Priority, Enabled: true, DataIdentifier: m.DataIdentifier, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
	}
	for _, s := range cfg.ProcessingScripts {
		if s.Enabled {
			content := s.Content
			row := model.DataScript{ID: uuid.NewString(), Name: s.Name, DeviceConfigID: id, EnableFlag: "Y", Content: &content, ScriptType: nativeRuntimeScriptTypes[s.Phase], CreatedAt: &now, UpdatedAt: &now}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
	}
	if _, err := brokermapper.NewRepository(tx).CompileForDeviceConfig(tx.Statement.Context, owner.TenantID, id); err != nil {
		return err
	}
	if err := tx.Exec("INSERT INTO native_runtime_managed_configurations(device_config_id,device_id,generation,content_digest) VALUES(?,?,?,?)", id, owner.DeviceID, in.Generation, in.Digest).Error; err != nil {
		return err
	}
	r := tx.Model(&model.Device{}).Where("id=? AND tenant_id=?", owner.DeviceID, owner.TenantID).Update("device_config_id", id)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrNativeRegistrationConflict
	}
	return tx.Exec(`INSERT INTO native_runtime_configurations(device_id,generation,content_digest,device_config_id,pending) VALUES(?,?,?,?,false)
 ON CONFLICT(device_id) DO UPDATE SET generation=EXCLUDED.generation,content_digest=EXCLUDED.content_digest,device_config_id=EXCLUDED.device_config_id,pending=false,applied_at=now()`, owner.DeviceID, in.Generation, in.Digest, id).Error
}

// First persist the admission fence, then drain all old grants before changing
// the projection. Returning pending must commit the fence, not roll it back.
func applyNativeRuntimeConfiguration(ctx context.Context, db *gorm.DB, tenant, deployment string, in NativeRuntimeLifecycle) error {
	if _, err := validateNativeRuntimeConfiguration(in.RuntimeConfiguration); err != nil {
		return err
	}
	if in.Generation != in.RuntimeConfiguration.Generation {
		return ErrNativeRegistrationInvalid
	}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	pending := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 72311031))", in.DeviceID).Error; err != nil {
			return err
		}
		var owner nativeRuntimeRegistration
		ownerErr := tx.Where("device_id=?", in.DeviceID).Take(&owner).Error
		if errors.Is(ownerErr, gorm.ErrRecordNotFound) {
			return ErrNativeRegistrationConflict
		}
		if ownerErr != nil {
			return ownerErr
		}
		if owner.Deleting || owner.NativeTenantID != tenant || owner.DeploymentID != deployment {
			return ErrNativeRegistrationConflict
		}
		var event nativeLifecycleEvent
		err := tx.Where("event_id=?", in.EventID).Take(&event).Error
		if err == nil {
			if event.DeviceID != in.DeviceID || event.RequestSHA256 != digest {
				return ErrNativeRegistrationConflict
			}
			if event.Applied {
				return nil
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		} else {
			var count int64
			if err := tx.Model(&nativeLifecycleEvent{}).Where("device_id=? AND applied=false", in.DeviceID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("previous runtime lifecycle still pending")
			}
			var current nativeRuntimeConfigurationRecord
			err := tx.Where("device_id=?", in.DeviceID).Take(&current).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && in.Generation == current.Generation && current.ContentDigest == in.RuntimeConfiguration.Digest && !current.Pending {
				// Registration can atomically apply a binding before its outbox
				// event arrives. Confirm the exact persisted projection, never
				// acknowledge a different same-generation snapshot.
				var count int64
				if err := tx.Model(&model.Device{}).Where("id=? AND tenant_id=? AND device_config_id=?", in.DeviceID, owner.TenantID, current.DeviceConfigID).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					return ErrNativeRegistrationConflict
				}
				return tx.Create(&nativeLifecycleEvent{EventID: in.EventID, DeviceID: in.DeviceID, RequestSHA256: digest, Generation: in.Generation, Applied: true}).Error
			}
			if err == nil && in.Generation <= current.Generation {
				return ErrNativeRegistrationConflict
			}
			if err := tx.Exec(`INSERT INTO native_runtime_configurations(device_id,generation,content_digest,device_config_id,pending) VALUES(?,?,?,'',true) ON CONFLICT(device_id) DO UPDATE SET generation=EXCLUDED.generation,content_digest=EXCLUDED.content_digest,pending=true`, in.DeviceID, in.Generation, in.RuntimeConfiguration.Digest).Error; err != nil {
				return err
			}
			event = nativeLifecycleEvent{EventID: in.EventID, DeviceID: in.DeviceID, RequestSHA256: digest, Generation: in.Generation}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
		}
		var leases int64
		if err := tx.Table("native_runtime_credential_leases").Where("device_id=? AND revoked_ack_at IS NULL AND expires_at>now()", in.DeviceID).Count(&leases).Error; err != nil {
			return err
		}
		if leases > 0 {
			pending = true
			return nil
		}
		if err := projectNativeRuntimeConfiguration(tx, owner, in.RuntimeConfiguration); err != nil {
			return err
		}
		return tx.Model(&event).Update("applied", true).Error
	})
	if err != nil {
		return err
	}
	if pending {
		return errors.New("runtime configuration broker leases still active")
	}
	return nil
}
