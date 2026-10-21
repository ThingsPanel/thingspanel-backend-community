package northbound

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"encore.app/internal/authz"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ListConnections returns this tenant's destination connections. Secrets are
// never included; only the names of the fields that are set.
//
//encore:api auth method=GET path=/v1/connections
func ListConnections(ctx context.Context) (*ConnectionListResponse, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionView); err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `
		SELECT c.id::text, c.name, c.connector_id, coalesce(i.capabilities->>'displayName', c.connector_id),
		       c.config, c.revision, c.state, coalesce(c.failure_code, ''), c.created_at, c.updated_at
		FROM nb_connections c
		LEFT JOIN nb_connector_installations i ON i.id = c.connector_id
		WHERE c.tenant_id = $1
		ORDER BY c.created_at DESC`, data.TenantID)
	if err != nil {
		return nil, errUnavailable("connection list unavailable")
	}
	defer rows.Close()
	out := &ConnectionListResponse{Connections: []ConnectionView{}}
	for rows.Next() {
		v, err := scanConnection(rows)
		if err != nil {
			return nil, errUnavailable("connection list unavailable")
		}
		v.SecretFields = secretFieldNames(ctx, data.TenantID, v.ID)
		out.Connections = append(out.Connections, v)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanConnection(row rowScanner) (ConnectionView, error) {
	var v ConnectionView
	var config []byte
	err := row.Scan(&v.ID, &v.Name, &v.ConnectorID, &v.ConnectorName, &config, &v.Revision,
		&v.State, &v.FailureCode, &v.CreatedAt, &v.UpdatedAt)
	v.Config = json.RawMessage(config)
	if len(v.Config) == 0 {
		v.Config = json.RawMessage(`{}`)
	}
	return v, err
}

// GetConnection returns one connection belonging to the caller's tenant.
//
//encore:api auth method=GET path=/v1/connections/:id
func GetConnection(ctx context.Context, id string) (*ConnectionView, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionView); err != nil {
		return nil, err
	}
	connID, err := parseUUID(id, "connection id")
	if err != nil {
		return nil, err
	}
	row := db.QueryRow(ctx, `
		SELECT c.id::text, c.name, c.connector_id, coalesce(i.capabilities->>'displayName', c.connector_id),
		       c.config, c.revision, c.state, coalesce(c.failure_code, ''), c.created_at, c.updated_at
		FROM nb_connections c
		LEFT JOIN nb_connector_installations i ON i.id = c.connector_id
		WHERE c.tenant_id = $1 AND c.id = $2::uuid`, data.TenantID, connID)
	v, err := scanConnection(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errNotFound("connection not found")
		}
		return nil, errUnavailable("connection unavailable")
	}
	v.SecretFields = secretFieldNames(ctx, data.TenantID, v.ID)
	return &v, nil
}

// CreateConnection stores a destination. The connector must be an installed
// official plugin; tenants cannot point the platform at an arbitrary URL.
//
//encore:api auth method=POST path=/v1/connections
func CreateConnection(ctx context.Context, req *ConnectionInput) (*ConnectionView, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionManage); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 120 {
		return nil, errBadRequest("name is required and must be at most 120 characters")
	}
	if _, _, err := installationByID(ctx, req.ConnectorID); err != nil {
		return nil, err
	}
	config := mustJSON(req.Config, "{}")
	if !json.Valid(config) {
		return nil, errBadRequest("config must be valid JSON")
	}
	id := uuid.NewString()
	_, err = db.Exec(ctx, `
		INSERT INTO nb_connections (id, tenant_id, name, connector_id, config, secret_ref, revision, state, created_by)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6, 1, 'active', $7)`,
		id, data.TenantID, name, req.ConnectorID, string(config), "nb-conn/"+id, data.UserID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errConflict("a connection with this name already exists")
		}
		return nil, errUnavailable("connection could not be created")
	}
	if err := storeSecrets(ctx, data.TenantID, id, req.Secrets); err != nil {
		return nil, err
	}
	return GetConnection(ctx, id)
}

// UpdateConnection replaces the configuration and bumps the revision. The
// caller must send the revision it read, so two people editing the same
// connection cannot silently overwrite each other.
//
//encore:api auth method=PUT path=/v1/connections/:id
func UpdateConnection(ctx context.Context, id string, req *ConnectionInput) (*ConnectionView, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionManage); err != nil {
		return nil, err
	}
	connID, err := parseUUID(id, "connection id")
	if err != nil {
		return nil, err
	}
	config := mustJSON(req.Config, "{}")
	if !json.Valid(config) {
		return nil, errBadRequest("config must be valid JSON")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errBadRequest("name is required")
	}
	var current int64
	var connectorID string
	err = db.QueryRow(ctx, `SELECT revision, connector_id FROM nb_connections WHERE tenant_id = $1 AND id = $2::uuid`,
		data.TenantID, connID).Scan(&current, &connectorID)
	if err != nil {
		return nil, errNotFound("connection not found")
	}
	if req.ExpectedRevision != current {
		return nil, errConflict("connection was changed by someone else; reload and retry")
	}
	targetConnector := req.ConnectorID
	if targetConnector == "" {
		targetConnector = connectorID
	}
	if targetConnector != connectorID {
		// Switching connector type changes the meaning of stored config, so it
		// is a new connection, not an edit.
		return nil, errConflict("connector type cannot be changed on an existing connection")
	}
	tag, err := db.Exec(ctx, `
		UPDATE nb_connections
		SET name = $3, config = $4::jsonb, revision = revision + 1, updated_at = now()
		WHERE tenant_id = $1 AND id = $2::uuid AND revision = $5`,
		data.TenantID, connID, name, string(config), req.ExpectedRevision)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errConflict("a connection with this name already exists")
		}
		return nil, errUnavailable("connection could not be updated")
	}
	if tag.RowsAffected() != 1 {
		return nil, errConflict("connection was changed by someone else; reload and retry")
	}
	if err := storeSecrets(ctx, data.TenantID, connID, req.Secrets); err != nil {
		return nil, err
	}
	return GetConnection(ctx, connID)
}

// DeleteConnection removes configuration only. It refuses while deliveries are
// still outstanding so a delete cannot silently discard a backlog.
//
//encore:api auth method=DELETE path=/v1/connections/:id
func DeleteConnection(ctx context.Context, id string) (*ConnectionView, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionManage); err != nil {
		return nil, err
	}
	connID, err := parseUUID(id, "connection id")
	if err != nil {
		return nil, err
	}
	view, err := GetConnection(ctx, connID)
	if err != nil {
		return nil, err
	}
	var pipelines int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM nb_pipelines WHERE tenant_id = $1 AND connection_id = $2::uuid`,
		data.TenantID, connID).Scan(&pipelines); err != nil {
		return nil, errUnavailable("connection delete check failed")
	}
	if pipelines > 0 {
		return nil, errConflict("this connection is still used by a forwarding task; delete the task first")
	}
	var pending int64
	if err := db.QueryRow(ctx, `SELECT count(*) FROM nb_deliveries
		WHERE tenant_id = $1 AND connection_id = $2::uuid AND state IN ('pending','leased','retry_wait','unknown')`,
		data.TenantID, connID).Scan(&pending); err != nil {
		return nil, errUnavailable("connection delete check failed")
	}
	if pending > 0 {
		return nil, errConflict("this connection still has undelivered records; they must be resolved first")
	}
	if _, err := db.Exec(ctx, `DELETE FROM nb_connections WHERE tenant_id = $1 AND id = $2::uuid`, data.TenantID, connID); err != nil {
		return nil, errUnavailable("connection could not be deleted")
	}
	return view, nil
}

// TestConnection asks the plugin to validate and open the destination. It is
// the only way the page learns whether a credential actually works.
//
//encore:api auth method=POST path=/v1/connections/:id/test
func TestConnection(ctx context.Context, id string, req *ConnectionTestRequest) (*ConnectionTestResponse, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionManage); err != nil {
		return nil, err
	}
	connID, err := parseUUID(id, "connection id")
	if err != nil {
		return nil, err
	}
	view, err := GetConnection(ctx, connID)
	if err != nil {
		return nil, err
	}
	inst, _, err := installationByID(ctx, view.ConnectorID)
	if err != nil {
		return nil, err
	}
	config := view.Config
	if len(req.Config) > 0 {
		if !json.Valid(req.Config) {
			return nil, errBadRequest("config must be valid JSON")
		}
		config = req.Config
	}
	secrets, err := loadSecrets(ctx, data.TenantID, connID)
	if err != nil {
		return nil, errConflict("stored credential cannot be read; re-enter it")
	}
	for k, v := range req.Secrets {
		secrets[k] = v
	}
	resp := &ConnectionTestResponse{Checks: []Check{}}

	desc, derr := describePlugin(ctx, inst)
	if derr != nil {
		resp.OK = false
		resp.State = "unreachable"
		resp.FailureCode = "connector_unreachable"
		resp.Message = "the connector process is not reachable"
		resp.Checks = append(resp.Checks, Check{Name: "connector", OK: false, Message: redact(derr.Error())})
		return resp, nil
	}
	resp.Checks = append(resp.Checks, Check{Name: "connector", OK: true, Message: desc.DisplayName + " " + desc.Version})

	validation, verr := validatePlugin(ctx, inst, config, secrets)
	if verr != nil {
		resp.State = "needs_repair"
		resp.FailureCode = "config_invalid"
		resp.Message = "the connector rejected the configuration"
		resp.Checks = append(resp.Checks, Check{Name: "config", OK: false, Message: redact(verr.Error())})
		return resp, nil
	}
	resp.Checks = append(resp.Checks, Check{Name: "config", OK: validation.OK, Message: validation.Message})
	if !validation.OK {
		resp.State = "needs_repair"
		resp.FailureCode = "config_invalid"
		resp.Message = validation.Message
		return resp, nil
	}
	opened, oerr := openPlugin(ctx, inst, connID, config, secrets)
	if oerr != nil || !opened.OK {
		resp.State = "needs_repair"
		resp.FailureCode = "destination_unreachable"
		resp.Message = "the destination could not be opened with these settings"
		msg := ""
		if oerr != nil {
			msg = redact(oerr.Error())
		} else {
			msg = opened.Message
		}
		resp.Checks = append(resp.Checks, Check{Name: "destination", OK: false, Message: msg})
		return resp, nil
	}
	resp.Checks = append(resp.Checks, Check{Name: "destination", OK: true, Message: "connection opened"})
	resp.OK = true
	resp.State = "active"
	_, _ = db.Exec(ctx, `UPDATE nb_connections SET state = 'active', failure_code = NULL, updated_at = $3
		WHERE tenant_id = $1 AND id = $2::uuid`, data.TenantID, connID, time.Now().UTC())
	return resp, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate key value violates unique constraint") ||
		strings.Contains(err.Error(), "SQLSTATE 23505")
}
