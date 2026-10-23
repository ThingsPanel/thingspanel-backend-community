package notification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"encore.app/l3-core/notification/contract"
	"github.com/google/uuid"
)

func (s *Store) CreateGroup(ctx context.Context, tenantID, idempotencyKey, requestDigest string, request contract.GroupCreate) (contract.GroupView, error) {
	if !validIdempotencyInput(tenantID, "group.create", idempotencyKey, requestDigest) || request.Name == "" {
		return contract.GroupView{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.GroupView{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	defer tx.Rollback()
	groupID := uuid.NewString()
	if _, replay, err := beginIdempotencyTx(ctx, tx, tenantID, "group.create", idempotencyKey, requestDigest, "group", groupID, 1); err != nil {
		return contract.GroupView{}, err
	} else if replay {
		var replayID string
		if err := tx.QueryRowContext(ctx, `SELECT resource_id FROM notification_idempotency WHERE tenant_id=$1 AND operation='group.create' AND idempotency_key=$2`, tenantID, idempotencyKey).Scan(&replayID); err != nil {
			return contract.GroupView{}, classifyStoreError(err)
		}
		if err := tx.Commit(); err != nil {
			return contract.GroupView{}, classifyStoreError(err)
		}
		return s.getGroupVersion(ctx, tenantID, replayID, 1)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_groups(tenant_id,id,current_version,current_revision) VALUES($1,$2,1,1)`, tenantID, groupID); err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_group_revisions(tenant_id,group_id,revision,version,name,enabled) VALUES($1,$2,1,1,$3,$4)`, tenantID, groupID, request.Name, request.Enabled); err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	if err := s.insertGroupBindings(ctx, tx, tenantID, groupID, 1, request.Enabled, request.Bindings); err != nil {
		return contract.GroupView{}, err
	}
	if err := tx.Commit(); err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	return s.getGroupVersion(ctx, tenantID, groupID, 1)
}

func (s *Store) UpdateGroup(ctx context.Context, tenantID, groupID, idempotencyKey, requestDigest string, request contract.GroupUpdate) (contract.GroupView, error) {
	if !validIdempotencyInput(tenantID, "group.update:"+groupID, idempotencyKey, requestDigest) || groupID == "" || request.ExpectedVersion < 1 || request.Name == "" {
		return contract.GroupView{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.GroupView{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	defer tx.Rollback()
	if record, err := findIdempotencyTx(ctx, tx, tenantID, "group.update:"+groupID, idempotencyKey); err != nil {
		return contract.GroupView{}, err
	} else if record != nil {
		if record.Digest != requestDigest {
			return contract.GroupView{}, storeErr(StoreErrorConflict)
		}
		if err := tx.Commit(); err != nil {
			return contract.GroupView{}, classifyStoreError(err)
		}
		return s.getGroupVersion(ctx, tenantID, record.ResourceID, record.ResourceVersion)
	}
	var current int64
	if err := tx.QueryRowContext(ctx, `SELECT current_version FROM notification_groups WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, groupID).Scan(&current); err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	if record, err := findIdempotencyTx(ctx, tx, tenantID, "group.update:"+groupID, idempotencyKey); err != nil {
		return contract.GroupView{}, err
	} else if record != nil {
		if record.Digest != requestDigest {
			return contract.GroupView{}, storeErr(StoreErrorConflict)
		}
		if err := tx.Commit(); err != nil {
			return contract.GroupView{}, classifyStoreError(err)
		}
		return s.getGroupVersion(ctx, tenantID, record.ResourceID, record.ResourceVersion)
	}
	if current != request.ExpectedVersion {
		return contract.GroupView{}, storeErr(StoreErrorVersionConflict)
	}
	next := current + 1
	if _, _, err := beginIdempotencyTx(ctx, tx, tenantID, "group.update:"+groupID, idempotencyKey, requestDigest, "group", groupID, next); err != nil {
		return contract.GroupView{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_group_revisions(tenant_id,group_id,revision,version,name,enabled) VALUES($1,$2,$3,$3,$4,$5)`, tenantID, groupID, next, request.Name, request.Enabled); err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	if err := s.insertGroupBindings(ctx, tx, tenantID, groupID, next, request.Enabled, request.Bindings); err != nil {
		return contract.GroupView{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_groups SET current_version=$3,current_revision=$3,updated_at=NOW() WHERE tenant_id=$1 AND id=$2`, tenantID, groupID, next); err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	return s.getGroupVersion(ctx, tenantID, groupID, next)
}

func (s *Store) GetGroup(ctx context.Context, tenantID, groupID string) (contract.GroupView, error) {
	return s.getGroupVersion(ctx, tenantID, groupID, 0)
}

func (s *Store) ResolvePublished(ctx context.Context, tenantID, groupID string, revision int64) (contract.GroupView, error) {
	view, err := s.getGroupVersion(ctx, tenantID, groupID, revision)
	if err != nil {
		return contract.GroupView{}, err
	}
	if !view.Enabled {
		return contract.GroupView{}, storeErr(StoreErrorNotFound)
	}
	return view, nil
}

func (s *Store) ListGroups(ctx context.Context, tenantID string) ([]contract.GroupView, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id,current_revision FROM notification_groups WHERE tenant_id=$1 ORDER BY updated_at DESC,id`, tenantID)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	defer rows.Close()
	type key struct {
		id  string
		rev int64
	}
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.rev); err != nil {
			return nil, classifyStoreError(err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyStoreError(err)
	}
	views := make([]contract.GroupView, 0, len(keys))
	for _, k := range keys {
		v, err := s.getGroupVersion(ctx, tenantID, k.id, k.rev)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, nil
}

func (s *Store) getGroupVersion(ctx context.Context, tenantID, groupID string, revision int64) (contract.GroupView, error) {
	db, err := s.database()
	if err != nil {
		return contract.GroupView{}, err
	}
	var view contract.GroupView
	err = db.QueryRowContext(ctx, `SELECT group_id,name,enabled,revision,version FROM notification_group_revisions WHERE tenant_id=$1 AND group_id=$2 AND revision=COALESCE(NULLIF($3,0),(SELECT current_revision FROM notification_groups WHERE tenant_id=$1 AND id=$2))`, tenantID, groupID, revision).Scan(&view.ID, &view.Name, &view.Enabled, &view.Revision, &view.Version)
	if err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT binding_id,instance_id,recipient_source_key_ref,recipient_source_ciphertext,recipient_source_aad_version,content_binding_key_ref,content_binding_ciphertext,content_binding_aad_version FROM notification_group_bindings WHERE tenant_id=$1 AND group_id=$2 AND group_revision=$3 ORDER BY binding_id`, tenantID, groupID, view.Revision)
	if err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var b contract.GroupBinding
		var rkey, ckey string
		var rc, cc []byte
		var rav, cav int
		if err := rows.Scan(&b.BindingID, &b.InstanceID, &rkey, &rc, &rav, &ckey, &cc, &cav); err != nil {
			return contract.GroupView{}, classifyStoreError(err)
		}
		raw, err := s.open(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: groupID, Version: view.Revision, Purpose: "group-binding-recipient", Slot: b.BindingID}, ProtectedSnapshot{KeyRef: rkey, Ciphertext: rc, AADVersion: rav})
		if err != nil {
			return contract.GroupView{}, storeErr(StoreErrorUnavailable)
		}
		b.RecipientSource = raw
		raw, err = s.open(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: groupID, Version: view.Revision, Purpose: "group-binding-content", Slot: b.BindingID}, ProtectedSnapshot{KeyRef: ckey, Ciphertext: cc, AADVersion: cav})
		if err != nil {
			return contract.GroupView{}, storeErr(StoreErrorUnavailable)
		}
		b.ContentBinding = raw
		view.Bindings = append(view.Bindings, b)
	}
	if err := rows.Err(); err != nil {
		return contract.GroupView{}, classifyStoreError(err)
	}
	if view.Bindings == nil {
		view.Bindings = []contract.GroupBinding{}
	}
	view.MigrationState = "native"
	return view, nil
}

func (s *Store) insertGroupBindings(ctx context.Context, tx *sql.Tx, tenantID, groupID string, revision int64, enabled bool, bindings []contract.GroupBinding) error {
	seen := map[string]bool{}
	for _, b := range bindings {
		if b.BindingID == "" || b.InstanceID == "" || seen[b.BindingID] || !json.Valid(b.RecipientSource) || !json.Valid(b.ContentBinding) {
			return storeErr(StoreErrorInvalid)
		}
		seen[b.BindingID] = true
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_instances WHERE tenant_id=$1 AND id=$2 AND (NOT $3 OR enabled=TRUE))`, tenantID, b.InstanceID, enabled).Scan(&exists); err != nil {
			return classifyStoreError(err)
		}
		if !exists {
			return storeErr(StoreErrorNotFound)
		}
		r, err := s.seal(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: groupID, Version: revision, Purpose: "group-binding-recipient", Slot: b.BindingID}, b.RecipientSource)
		if err != nil {
			return err
		}
		c, err := s.seal(ctx, SnapshotAAD{ScopeKind: "tenant", TenantID: tenantID, ResourceID: groupID, Version: revision, Purpose: "group-binding-content", Slot: b.BindingID}, b.ContentBinding)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO notification_group_bindings(tenant_id,group_id,group_revision,binding_id,instance_id,recipient_source_key_ref,recipient_source_ciphertext,recipient_source_aad_version,content_binding_key_ref,content_binding_ciphertext,content_binding_aad_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, tenantID, groupID, revision, b.BindingID, b.InstanceID, r.KeyRef, r.Ciphertext, r.AADVersion, c.KeyRef, c.Ciphertext, c.AADVersion); err != nil {
			return classifyStoreError(err)
		}
	}
	return nil
}

func (s *Store) RegisterGroupProjectionIdempotent(ctx context.Context, principal SourceProjectionPrincipal, key, digest string, request contract.SourceGroupProjectionRequest) (contract.SourceGroupProjection, error) {
	if len(principal.TenantIDs) == 0 || !containsString(principal.TenantIDs, request.TenantID) || principal.DeploymentID == "" || principal.DeploymentID != request.SourceDeploymentID || request.TenantID == "" || request.LegacyGroupID == "" || request.NotificationGroupID == "" || request.GroupRevision < 1 || !validIdempotencyInput(request.TenantID, "source-group-projection", key, digest) {
		return contract.SourceGroupProjection{}, storeErr(StoreErrorInvalid)
	}
	db, err := s.database()
	if err != nil {
		return contract.SourceGroupProjection{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return contract.SourceGroupProjection{}, classifyStoreError(err)
	}
	defer tx.Rollback()
	if rec, err := findIdempotencyTx(ctx, tx, request.TenantID, "source-group-projection", key); err != nil {
		return contract.SourceGroupProjection{}, err
	} else if rec != nil {
		if rec.Digest != digest {
			return contract.SourceGroupProjection{}, storeErr(StoreErrorConflict)
		}
		if err := tx.Commit(); err != nil {
			return contract.SourceGroupProjection{}, classifyStoreError(err)
		}
		return s.getProjection(ctx, request.SourceDeploymentID, request.TenantID, request.LegacyGroupID, request.GroupRevision)
	}
	var currentGroup string
	err = tx.QueryRowContext(ctx, `SELECT notification_group_id FROM notification_source_group_mappings WHERE source_deployment_id=$1 AND tenant_id=$2 AND legacy_group_id=$3 FOR UPDATE`, request.SourceDeploymentID, request.TenantID, request.LegacyGroupID).Scan(&currentGroup)
	if err == sql.ErrNoRows {
		if _, err := tx.ExecContext(ctx, `INSERT INTO notification_source_group_mappings(source_deployment_id,tenant_id,legacy_group_id,notification_group_id) VALUES($1,$2,$3,$4)`, request.SourceDeploymentID, request.TenantID, request.LegacyGroupID, request.NotificationGroupID); err != nil {
			return contract.SourceGroupProjection{}, classifyStoreError(err)
		}
	} else if err != nil {
		return contract.SourceGroupProjection{}, classifyStoreError(err)
	} else if currentGroup != request.NotificationGroupID {
		return contract.SourceGroupProjection{}, storeErr(StoreErrorConflict)
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_group_revisions WHERE tenant_id=$1 AND group_id=$2 AND revision=$3 AND enabled=TRUE)`, request.TenantID, request.NotificationGroupID, request.GroupRevision).Scan(&exists); err != nil {
		return contract.SourceGroupProjection{}, classifyStoreError(err)
	}
	if !exists {
		return contract.SourceGroupProjection{}, storeErr(StoreErrorNotFound)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM notification_source_group_revisions WHERE source_deployment_id=$1 AND tenant_id=$2 AND legacy_group_id=$3`, request.SourceDeploymentID, request.TenantID, request.LegacyGroupID).Scan(&count); err != nil {
		return contract.SourceGroupProjection{}, classifyStoreError(err)
	}
	var alreadyRecorded bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_source_group_revisions WHERE source_deployment_id=$1 AND tenant_id=$2 AND legacy_group_id=$3 AND group_revision=$4)`, request.SourceDeploymentID, request.TenantID, request.LegacyGroupID, request.GroupRevision).Scan(&alreadyRecorded); err != nil {
		return contract.SourceGroupProjection{}, classifyStoreError(err)
	}
	if count >= contract.MaxSourceGroupProjectionRevisions && !alreadyRecorded {
		return contract.SourceGroupProjection{}, storeErr(StoreErrorConflict)
	}
	if _, _, err := beginIdempotencyTx(ctx, tx, request.TenantID, "source-group-projection", key, digest, "projection", request.NotificationGroupID, request.GroupRevision); err != nil {
		return contract.SourceGroupProjection{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_source_group_revisions(source_deployment_id,tenant_id,legacy_group_id,notification_group_id,group_revision) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, request.SourceDeploymentID, request.TenantID, request.LegacyGroupID, request.NotificationGroupID, request.GroupRevision); err != nil {
		return contract.SourceGroupProjection{}, classifyStoreError(err)
	}
	if err := tx.Commit(); err != nil {
		return contract.SourceGroupProjection{}, classifyStoreError(err)
	}
	return s.getProjection(ctx, request.SourceDeploymentID, request.TenantID, request.LegacyGroupID, request.GroupRevision)
}

func (s *Store) RegisterGroupProjection(ctx context.Context, principal SourceProjectionPrincipal, request contract.SourceGroupProjectionRequest) (contract.SourceGroupProjection, error) {
	wire, err := json.Marshal(request)
	if err != nil {
		return contract.SourceGroupProjection{}, storeErr(StoreErrorInvalid)
	}
	digestBytes := sha256.Sum256(wire)
	digest := hex.EncodeToString(digestBytes[:])
	keyBytes := sha256.Sum256([]byte(request.SourceDeploymentID + "\x00" + request.TenantID + "\x00" + request.LegacyGroupID + "\x00" + itoa(int(request.GroupRevision))))
	key := "projection-" + hex.EncodeToString(keyBytes[:])
	return s.RegisterGroupProjectionIdempotent(ctx, principal, key, digest, request)
}
func (s *Store) ListGroupProjections(ctx context.Context, tenantID, groupID string) ([]contract.SourceGroupProjection, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT source_deployment_id,tenant_id,legacy_group_id,notification_group_id,group_revision,created_at FROM notification_source_group_revisions WHERE tenant_id=$1 AND notification_group_id=$2 ORDER BY source_deployment_id,legacy_group_id,group_revision`, tenantID, groupID)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	defer rows.Close()
	var out []contract.SourceGroupProjection
	for rows.Next() {
		var v contract.SourceGroupProjection
		var created sql.NullTime
		if err := rows.Scan(&v.SourceDeploymentID, &v.TenantID, &v.LegacyGroupID, &v.NotificationGroupID, &v.GroupRevision, &created); err != nil {
			return nil, classifyStoreError(err)
		}
		if created.Valid {
			v.CreatedAt = created.Time.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyStoreError(err)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].GroupRevision < out[j].GroupRevision })
	if out == nil {
		out = []contract.SourceGroupProjection{}
	}
	return out, nil
}

func (s *Store) getProjection(ctx context.Context, deployment, tenant, legacy string, revision int64) (contract.SourceGroupProjection, error) {
	db, err := s.database()
	if err != nil {
		return contract.SourceGroupProjection{}, err
	}
	var v contract.SourceGroupProjection
	var created sql.NullTime
	err = db.QueryRowContext(ctx, `SELECT source_deployment_id,tenant_id,legacy_group_id,notification_group_id,group_revision,created_at FROM notification_source_group_revisions WHERE source_deployment_id=$1 AND tenant_id=$2 AND legacy_group_id=$3 AND group_revision=$4`, deployment, tenant, legacy, revision).Scan(&v.SourceDeploymentID, &v.TenantID, &v.LegacyGroupID, &v.NotificationGroupID, &v.GroupRevision, &created)
	if err != nil {
		return contract.SourceGroupProjection{}, classifyStoreError(err)
	}
	if created.Valid {
		v.CreatedAt = created.Time.UTC().Format(time.RFC3339Nano)
	}
	return v, nil
}
