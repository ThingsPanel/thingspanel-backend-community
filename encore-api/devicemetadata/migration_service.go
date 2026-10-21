package devicemetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

// ─── Migration constants ─────────────────────────────────────────────────────

const (
	MigrationTypeLegacyTemplate = "LEGACY_TEMPLATE_TO_THING_MODEL"
	MigrationTypeDeviceTopic    = "DEVICE_TOPIC_TO_DEVICES"

	MigrationStatusSuccess    = "SUCCESS"
	MigrationStatusRolledBack = "ROLLBACKED"
	MigrationStatusFailed     = "FAILED"

	TopicProfileLegacy         = "legacy"
	TopicProfileDevices        = "devices"
	TopicProfileDevicesPending = "devices_pending"
)

// ─── Migration models ────────────────────────────────────────────────────────

// MigrationLog records every upgrade action and enables rollback.
type MigrationLog struct {
	ID                 string          `json:"id"`
	TenantID           string          `json:"tenant_id"`
	Type               string          `json:"type"`
	SourceID           string          `json:"source_id,omitempty"`
	TargetThingModelID *string         `json:"target_thing_model_id,omitempty"`
	TargetTemplateID   *string         `json:"target_template_id,omitempty"`
	Payload            json.RawMessage `json:"payload"`
	Status             string          `json:"status"`
	CreatedAt          time.Time       `json:"created_at"`
	CreatedBy          *string         `json:"created_by,omitempty"`
	RolledbackAt       *time.Time      `json:"rolledback_at,omitempty"`
}

// MigrationPayload holds details written into migration_log.payload.
type MigrationPayload struct {
	ItemsCount int                `json:"items_count,omitempty"`
	Renames    []IdentifierRename `json:"renames,omitempty"`
	Warnings   []string           `json:"warnings,omitempty"`
	Strategy   string             `json:"strategy,omitempty"`
}

// IdentifierRename records when an identifier was renamed to avoid collision.
type IdentifierRename struct {
	OldIdentifier string `json:"old_identifier"`
	NewIdentifier string `json:"new_identifier"`
	Reason        string `json:"reason"`
}

// MigrateLegacyTemplateReq is the request for migrating a legacy device template.
type MigrateLegacyTemplateReq struct {
	ThingModelName string `json:"thing_model_name,omitempty"`
	DryRun         bool   `json:"dry_run"`
}

// MigrateLegacyTemplateResp is returned from a successful migration.
type MigrateLegacyTemplateResp struct {
	MigrationID  string             `json:"migration_id"`
	ThingModelID string             `json:"thing_model_id"`
	ItemsCount   int                `json:"items_count"`
	Renames      []IdentifierRename `json:"renames,omitempty"`
	Warnings     []string           `json:"warnings,omitempty"`
	DryRun       bool               `json:"dry_run"`
}

// MigrateDeviceTopicReq is the request for switching a device to devices/ topic.
type MigrateDeviceTopicReq struct {
	// "legacy_adapter" – mark in DB only, backend adapter handles routing
	// "device_sdk_update" – send downlink command to device
	Strategy string `json:"strategy"`
}

// MigrateDeviceTopicResp is returned from a successful topic migration.
type MigrateDeviceTopicResp struct {
	MigrationID string `json:"migration_id"`
	Strategy    string `json:"strategy"`
}

// RollbackMigrationResp is returned from a successful rollback.
type RollbackMigrationResp struct {
	RolledBack bool `json:"rolled_back"`
}

// ListMigrationHistoryReq supports pagination + type filter.
type ListMigrationHistoryReq struct {
	pageRequest
	Type string `query:"type"`
}

// ─── API handlers ────────────────────────────────────────────────────────────

// MigrateFromLegacyTemplate converts a legacy device template into a new ThingModel.
// If dry_run=true it returns a preview without writing to the database.
//
//encore:api auth method=POST path=/api/migrate/from-legacy-template/:legacyTemplateID
func (s *Service) MigrateFromLegacyTemplate(ctx context.Context, legacyTemplateID string, req *MigrateLegacyTemplateReq) (*MigrateLegacyTemplateResp, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	actorID := currentUserID()

	tpl, err := getDeviceTemplate(ctx, tenantID, legacyTemplateID)
	if err != nil {
		return nil, errs.WrapCode(err, errs.NotFound, "device template not found")
	}

	tmName := req.ThingModelName
	if tmName == "" {
		tmName = tpl.Name + " (auto)"
	}

	items, renames, warnings := buildLegacyItems(ctx, tpl.ID)

	if req.DryRun {
		return &MigrateLegacyTemplateResp{
			ThingModelID: "(dry-run)",
			ItemsCount:   len(items),
			Renames:      renames,
			Warnings:     warnings,
			DryRun:       true,
		}, nil
	}

	tx, txErr := db.Begin(ctx)
	if txErr != nil {
		return nil, errs.WrapCode(txErr, errs.Internal, "begin tx")
	}
	defer tx.Rollback()

	// 1. Create ThingModel
	tmID := uuid.New().String()
	if _, err = tx.Exec(ctx, `
		INSERT INTO thing_models (id, tenant_id, name, status, created_by, updated_by)
		VALUES ($1, $2, $3, 'DRAFT', $4, $4)`,
		tmID, tenantID, tmName, actorID); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "create thing model")
	}

	// 2. Insert items
	for i := range items {
		items[i].ID = uuid.New().String()
		items[i].ThingModelID = tmID
		items[i].TenantID = tenantID
		if err = insertThingModelItemTx(ctx, tx, tenantID, tmID, &items[i]); err != nil {
			return nil, errs.WrapCode(err, errs.Internal, fmt.Sprintf("insert item %s", items[i].Identifier))
		}
	}

	// 3. Publish snapshot
	snapID := uuid.New().String()
	content, err := collectSnapshotContentTx(ctx, tx, tmID, tenantID, 1)
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "collect snapshot")
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO thing_model_snapshots (id, thing_model_id, tenant_id, version, content, changelog, published_by)
		VALUES ($1, $2, $3, 1, $4, 'Migrated from legacy template', $5)`,
		snapID, tmID, tenantID, content, actorID); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "create snapshot")
	}

	// 4. Mark ThingModel as PUBLISHED
	if _, err = tx.Exec(ctx, `
		UPDATE thing_models SET status = 'PUBLISHED', current_snapshot_id = $1, updated_at = NOW()
		WHERE id = $2 AND tenant_id = $3`,
		snapID, tmID, tenantID); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "publish thing model")
	}

	// 5. Bind device_template to new thing_model (only the 2 allowed new columns)
	if _, err = tx.Exec(ctx, `
		UPDATE device_templates
		SET bound_thing_model_id = $1, bound_thing_model_snapshot_id = $2, updated_at = NOW()
		WHERE id = $3 AND tenant_id = $4`,
		tmID, snapID, legacyTemplateID, tenantID); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "bind thing model to template")
	}

	// 6. Write migration_log
	payload, _ := json.Marshal(MigrationPayload{
		ItemsCount: len(items),
		Renames:    renames,
		Warnings:   warnings,
	})
	migLogID := uuid.New().String()
	if _, err = tx.Exec(ctx, `
		INSERT INTO migration_log (id, tenant_id, type, source_id, target_thing_model_id, target_template_id, payload, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'SUCCESS', $8)`,
		migLogID, tenantID, MigrationTypeLegacyTemplate, legacyTemplateID, tmID, legacyTemplateID, payload, actorID); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "write migration log")
	}

	if err = tx.Commit(); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "commit tx")
	}

	return &MigrateLegacyTemplateResp{
		MigrationID:  migLogID,
		ThingModelID: tmID,
		ItemsCount:   len(items),
		Renames:      renames,
		Warnings:     warnings,
		DryRun:       false,
	}, nil
}

// MigrateDeviceTopic switches a device to use the new devices/{sn}/... topic profile.
//
//encore:api auth method=POST path=/api/migrate/device-topic/:deviceID
func (s *Service) MigrateDeviceTopic(ctx context.Context, deviceID string, req *MigrateDeviceTopicReq) (*MigrateDeviceTopicResp, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	actorID := currentUserID()

	if req.Strategy != "legacy_adapter" && req.Strategy != "device_sdk_update" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "strategy must be 'legacy_adapter' or 'device_sdk_update'"}
	}

	_, err = getDevice(ctx, tenantID, deviceID)
	if err != nil {
		return nil, errs.WrapCode(err, errs.NotFound, "device not found")
	}

	newProfile := TopicProfileDevices
	if req.Strategy == "device_sdk_update" {
		newProfile = TopicProfileDevicesPending
	}

	tx, txErr := db.Begin(ctx)
	if txErr != nil {
		return nil, errs.WrapCode(txErr, errs.Internal, "begin tx")
	}
	defer tx.Rollback()

	if _, err = tx.Exec(ctx, `
		UPDATE devices SET topic_profile = $1 WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`,
		newProfile, deviceID, tenantID); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "update topic_profile")
	}

	payload, _ := json.Marshal(MigrationPayload{Strategy: req.Strategy})
	migLogID := uuid.New().String()
	if _, err = tx.Exec(ctx, `
		INSERT INTO migration_log (id, tenant_id, type, source_id, payload, status, created_by)
		VALUES ($1, $2, $3, $4, $5, 'SUCCESS', $6)`,
		migLogID, tenantID, MigrationTypeDeviceTopic, deviceID, payload, actorID); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "write migration log")
	}

	if err = tx.Commit(); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "commit tx")
	}

	return &MigrateDeviceTopicResp{
		MigrationID: migLogID,
		Strategy:    req.Strategy,
	}, nil
}

// RollbackMigration undoes a previous migration.
//
//encore:api auth method=POST path=/api/migrate/rollback/:migrationID
func (s *Service) RollbackMigration(ctx context.Context, migrationID string) (*RollbackMigrationResp, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}

	ml, err := getMigrationLog(ctx, tenantID, migrationID)
	if err != nil {
		return nil, errs.WrapCode(err, errs.NotFound, "migration not found")
	}
	if ml.Status == MigrationStatusRolledBack {
		return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "migration already rolled back"}
	}

	tx, txErr := db.Begin(ctx)
	if txErr != nil {
		return nil, errs.WrapCode(txErr, errs.Internal, "begin tx")
	}
	defer tx.Rollback()

	switch ml.Type {
	case MigrationTypeLegacyTemplate:
		if ml.TargetThingModelID != nil {
			if _, err = tx.Exec(ctx, `
				UPDATE thing_models SET deleted_at = NOW() WHERE id = $1 AND tenant_id = $2`,
				*ml.TargetThingModelID, tenantID); err != nil {
				return nil, errs.WrapCode(err, errs.Internal, "soft delete thing model")
			}
		}
		if ml.SourceID != "" {
			if _, err = tx.Exec(ctx, `
				UPDATE device_templates
				SET bound_thing_model_id = NULL, bound_thing_model_snapshot_id = NULL, updated_at = NOW()
				WHERE id = $1 AND tenant_id = $2`,
				ml.SourceID, tenantID); err != nil {
				return nil, errs.WrapCode(err, errs.Internal, "unbind thing model from template")
			}
		}

	case MigrationTypeDeviceTopic:
		if ml.SourceID != "" {
			if _, err = tx.Exec(ctx, `
				UPDATE devices SET topic_profile = $1 WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`,
				TopicProfileLegacy, ml.SourceID, tenantID); err != nil {
				return nil, errs.WrapCode(err, errs.Internal, "restore topic_profile")
			}
		}

	default:
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: fmt.Sprintf("unknown migration type: %s", ml.Type)}
	}

	now := time.Now()
	if _, err = tx.Exec(ctx, `
		UPDATE migration_log SET status = 'ROLLBACKED', rolledback_at = $1 WHERE id = $2 AND tenant_id = $3`,
		now, migrationID, tenantID); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "mark migration rolled back")
	}

	if err = tx.Commit(); err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "commit tx")
	}

	return &RollbackMigrationResp{RolledBack: true}, nil
}

// ListMigrationHistory returns all migration records for the current tenant.
//
//encore:api auth method=GET path=/api/migrate/history
func (s *Service) ListMigrationHistory(ctx context.Context, req *ListMigrationHistoryReq) (*listResponse[MigrationLog], error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}

	req.pageRequest.normalize()
	logs, total, err := listMigrationLogs(ctx, tenantID, req.Type, req.pageRequest)
	if err != nil {
		return nil, errs.WrapCode(err, errs.Internal, "list migration logs")
	}
	return &listResponse[MigrationLog]{
		Items:    logs,
		Page:     req.Page,
		PageSize: req.PageSize,
		Total:    total,
	}, nil
}

// ─── Store helpers ───────────────────────────────────────────────────────────

func getMigrationLog(ctx context.Context, tenantID, id string) (*MigrationLog, error) {
	row := db.QueryRow(ctx, `
		SELECT id, tenant_id, type, source_id, target_thing_model_id, target_template_id,
		       payload, status, created_at, created_by, rolledback_at
		FROM migration_log
		WHERE id = $1 AND tenant_id = $2`, id, tenantID)

	ml := &MigrationLog{}
	err := row.Scan(
		&ml.ID, &ml.TenantID, &ml.Type, &ml.SourceID,
		&ml.TargetThingModelID, &ml.TargetTemplateID,
		&ml.Payload, &ml.Status, &ml.CreatedAt, &ml.CreatedBy, &ml.RolledbackAt,
	)
	if err != nil {
		return nil, wrapDBErr(err, "migration log not found")
	}
	return ml, nil
}

func listMigrationLogs(ctx context.Context, tenantID, migrationType string, page pageRequest) ([]MigrationLog, int, error) {
	args := []any{tenantID}
	typeFilter := ""
	if migrationType != "" {
		args = append(args, migrationType)
		typeFilter = fmt.Sprintf("AND type = $%d", len(args))
	}

	var total int
	countQ := fmt.Sprintf(`SELECT COUNT(*) FROM migration_log WHERE tenant_id = $1 AND (rolledback_at IS NULL OR status != 'ROLLBACKED') %s`, typeFilter)
	if err := db.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := page.PageSize
	offset := (page.Page - 1) * page.PageSize
	args = append(args, limit, offset)
	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, type, source_id, target_thing_model_id, target_template_id,
		       payload, status, created_at, created_by, rolledback_at
		FROM migration_log
		WHERE tenant_id = $1 %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, typeFilter, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []MigrationLog
	for rows.Next() {
		var ml MigrationLog
		if err := rows.Scan(
			&ml.ID, &ml.TenantID, &ml.Type, &ml.SourceID,
			&ml.TargetThingModelID, &ml.TargetTemplateID,
			&ml.Payload, &ml.Status, &ml.CreatedAt, &ml.CreatedBy, &ml.RolledbackAt,
		); err != nil {
			return nil, 0, err
		}
		list = append(list, ml)
	}
	return list, total, rows.Err()
}

// buildLegacyItems produces ThingModelItems by reading the four v1 model sub-tables
// from the ThingsPanel main database (requires TP_LEGACY_DATABASE_URL to be set).
func buildLegacyItems(ctx context.Context, templateID string) (items []ThingModelItem, renames []IdentifierRename, warnings []string) {
	raw, err := readLegacyModelItems(ctx, templateID)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("could not read legacy model items: %v", err))
		return
	}
	items, renames, warnings = convertLegacyItems(raw)
	return
}

// ─── Batch migration from legacy ThingsPanel DB ──────────────────────────────

// BatchMigrateFromLegacyDBResp is returned from a batch migration.
type BatchMigrateFromLegacyDBResp struct {
	Migrated []string `json:"migrated"`
	Skipped  []string `json:"skipped"`
	Errors   []string `json:"errors"`
}

// MigrateAllLegacyTemplates imports all legacy device templates from the ThingsPanel
// main database (TP_LEGACY_DATABASE_URL) into new ThingModels for the current tenant.
// Already-migrated templates (present in migration_log) are skipped.
//
//encore:api auth method=POST path=/api/migrate/all-legacy-templates
func (s *Service) MigrateAllLegacyTemplates(ctx context.Context) (*BatchMigrateFromLegacyDBResp, error) {
	// Use raw (non-normalized) tenant_id for legacy DB queries — legacy DB stores short hex like "d616bcbb"
	rawTenantID, err := mustRawTenantID()
	if err != nil {
		return nil, err
	}
	// Use normalized tenant_id for inserts into the new device_metadata DB
	tenantID, err := mustTenantID()
	if err != nil {
		return nil, err
	}
	actorID := currentUserID()

	legacyTemplates, err := listLegacyTemplates(ctx, rawTenantID)
	if err != nil {
		return nil, errs.WrapCode(err, errs.Unavailable, "legacy DB unavailable: "+err.Error())
	}

	// Build set of already-migrated source IDs from migration_log
	migratedRows, _ := db.Query(ctx,
		`SELECT source_id FROM migration_log WHERE tenant_id=$1 AND type=$2 AND status='SUCCESS'`,
		tenantID, MigrationTypeLegacyTemplate)
	migrated := map[string]bool{}
	if migratedRows != nil {
		for migratedRows.Next() {
			var sid string
			_ = migratedRows.Scan(&sid)
			migrated[sid] = true
		}
		migratedRows.Close()
	}

	resp := &BatchMigrateFromLegacyDBResp{}

	for _, ltpl := range legacyTemplates {
		if migrated[ltpl.ID] {
			resp.Skipped = append(resp.Skipped, ltpl.ID)
			continue
		}

		if mErr := migrateSingleLegacyTemplate(ctx, tenantID, actorID, ltpl); mErr != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s (%s): %v", ltpl.ID, ltpl.Name, mErr))
		} else {
			resp.Migrated = append(resp.Migrated, ltpl.ID)
		}
	}

	return resp, nil
}

func migrateSingleLegacyTemplate(ctx context.Context, tenantID, actorID string, ltpl legacyDeviceTemplate) error {
	tmName := ltpl.Name
	if tmName == "" {
		tmName = "Template " + ltpl.ID
	}

	rawItems, err := readLegacyModelItems(ctx, ltpl.ID)
	if err != nil {
		return fmt.Errorf("read items: %w", err)
	}
	items, renames, warnings := convertLegacyItems(rawItems)

	tx, txErr := db.Begin(ctx)
	if txErr != nil {
		return fmt.Errorf("begin tx: %w", txErr)
	}
	defer tx.Rollback()

	// Check for existing thing model with same name; append suffix if needed
	finalName := tmName
	var existing int
	_ = db.QueryRow(ctx,
		`SELECT COUNT(*) FROM thing_models WHERE tenant_id=$1 AND name=$2 AND deleted_at IS NULL`,
		tenantID, finalName).Scan(&existing)
	if existing > 0 {
		finalName = tmName + " (migrated)"
	}

	tmID := uuid.New().String()
	if _, err = tx.Exec(ctx, `
		INSERT INTO thing_models (id, tenant_id, name, status, created_by, updated_by)
		VALUES ($1, $2, $3, 'DRAFT', $4, $4)`,
		tmID, tenantID, finalName, actorID); err != nil {
		return fmt.Errorf("create thing model: %w", err)
	}

	for i := range items {
		items[i].ID = uuid.New().String()
		items[i].ThingModelID = tmID
		items[i].TenantID = tenantID
		if err = insertThingModelItemTx(ctx, tx, tenantID, tmID, &items[i]); err != nil {
			return fmt.Errorf("insert item %s: %w", items[i].Identifier, err)
		}
	}

	snapID := uuid.New().String()
	content, err := collectSnapshotContentTx(ctx, tx, tmID, tenantID, 1)
	if err != nil {
		return fmt.Errorf("collect snapshot: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO thing_model_snapshots (id, thing_model_id, tenant_id, version, content, changelog, published_by)
		VALUES ($1, $2, $3, 1, $4, 'Auto-migrated from legacy template', $5)`,
		snapID, tmID, tenantID, content, actorID); err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}

	if _, err = tx.Exec(ctx, `
		UPDATE thing_models SET status='PUBLISHED', current_snapshot_id=$1, updated_at=NOW()
		WHERE id=$2 AND tenant_id=$3`,
		snapID, tmID, tenantID); err != nil {
		return fmt.Errorf("publish thing model: %w", err)
	}

	payload, _ := json.Marshal(MigrationPayload{
		ItemsCount: len(items),
		Renames:    renames,
		Warnings:   warnings,
	})
	migLogID := uuid.New().String()
	if _, err = tx.Exec(ctx, `
		INSERT INTO migration_log (id, tenant_id, type, source_id, target_thing_model_id, payload, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'SUCCESS', $7)`,
		migLogID, tenantID, MigrationTypeLegacyTemplate, ltpl.ID, tmID, payload, actorID); err != nil {
		return fmt.Errorf("write migration log: %w", err)
	}

	// Migrate custom commands from legacy DB into the new thing model.
	// Errors here are non-fatal but must not be silently lost.
	customCmds, ccErr := readLegacyCustomCommands(ctx, ltpl.ID)
	if ccErr != nil {
		return fmt.Errorf("read custom commands: %w", ccErr)
	}
	for _, cmd := range customCmds {
		cmd.ID = uuid.New().String()
		cmd.ThingModelID = tmID
		cmd.TenantID = tenantID
		if _, err = tx.Exec(ctx, `
			INSERT INTO custom_command_controls (id, thing_model_id, tenant_id, name, description, content, enable_status, control_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			cmd.ID, tmID, tenantID, cmd.Name, cmd.Description, cmd.Content, cmd.EnableStatus, cmd.ControlType); err != nil {
			return fmt.Errorf("insert custom command %q: %w", cmd.Name, err)
		}
	}

	return tx.Commit()
}
