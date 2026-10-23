package northbound

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"encore.app/internal/authz"
)

// bootstrapInstallations registers the fixed official plugin set for this
// release. It is idempotent: restarting the service must not create a second
// installation row or forget the previous health.
func bootstrapInstallations(ctx context.Context, connectors []connectorConfig) error {
	for _, c := range connectors {
		if c.ID == "" || c.Endpoint == "" {
			continue
		}
		state := "ready"
		if c.Disabled {
			state = "revoked"
		}
		_, err := db.Exec(ctx, `
			INSERT INTO nb_connector_installations
				(id, version, contract_version, endpoint, artifact_digest, capabilities, state, updated_at)
			VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, $6, now())
			ON CONFLICT (id) DO UPDATE SET
				version = excluded.version,
				contract_version = excluded.contract_version,
				endpoint = excluded.endpoint,
				artifact_digest = excluded.artifact_digest,
				state = CASE WHEN excluded.state = 'revoked' THEN 'revoked' ELSE nb_connector_installations.state END,
				updated_at = now()`,
			c.ID, c.Version, ContractVersion, c.Endpoint, c.Digest, state)
		if err != nil {
			return err
		}
	}
	return nil
}

// probeInstallations refreshes descriptor and health. A plugin that is down is
// reported as unreachable instead of staying "installed".
func probeInstallations(ctx context.Context) {
	rows, err := db.Query(ctx, `SELECT id, version, endpoint, artifact_digest, state FROM nb_connector_installations`)
	if err != nil {
		log.Printf("northbound: probe query failed: %v", err)
		return
	}
	type inst struct{ id, version, endpoint, digest, state string }
	var list []inst
	for rows.Next() {
		var i inst
		if err := rows.Scan(&i.id, &i.version, &i.endpoint, &i.digest, &i.state); err != nil {
			rows.Close()
			return
		}
		list = append(list, i)
	}
	rows.Close()

	for _, i := range list {
		if i.state == "revoked" {
			continue
		}
		conf := connectorConfig{ID: i.id, Version: i.version, Endpoint: i.endpoint, Digest: i.digest, Token: tokenFor(i.id)}
		probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		desc, err := describePlugin(probeCtx, conf)
		cancel()
		if err != nil {
			_, _ = db.Exec(ctx, `UPDATE nb_connector_installations
				SET state = 'unreachable', last_error = $2, updated_at = now() WHERE id = $1`, i.id, redact(err.Error()))
			continue
		}
		raw, _ := json.Marshal(desc)
		_, _ = db.Exec(ctx, `UPDATE nb_connector_installations
			SET state = 'ready', capabilities = $2::jsonb, version = $3, last_seen_at = now(),
			    last_error = NULL, updated_at = now() WHERE id = $1`,
			i.id, string(raw), desc.Version)
	}
}

// tokenFor looks up the plugin-internal bearer token from configuration. It is
// never stored in the database.
func tokenFor(id string) string {
	for _, c := range cfg.Connectors {
		if c.ID == id {
			return c.Token
		}
	}
	return ""
}

func installationByID(ctx context.Context, id string) (connectorConfig, *Descriptor, error) {
	var endpoint, version, digest, state string
	var caps []byte
	err := db.QueryRow(ctx, `SELECT endpoint, version, artifact_digest, state, capabilities
		FROM nb_connector_installations WHERE id = $1`, id).Scan(&endpoint, &version, &digest, &state, &caps)
	if err != nil {
		return connectorConfig{}, nil, errNotFound("unknown connector " + id)
	}
	if state == "revoked" {
		return connectorConfig{}, nil, errConflict("connector " + id + " has been revoked")
	}
	inst := connectorConfig{ID: id, Version: version, Endpoint: endpoint, Digest: digest, Token: tokenFor(id)}
	var desc Descriptor
	if len(caps) > 0 {
		_ = json.Unmarshal(caps, &desc)
	}
	return inst, &desc, nil
}

func toConnectorView(id, version, contractVersion, endpoint, digest, state string, caps []byte, lastSeen *time.Time, lastErr *string) ConnectorView {
	var desc Descriptor
	if len(caps) > 0 {
		_ = json.Unmarshal(caps, &desc)
	}
	name := desc.DisplayName
	if name == "" {
		name = id
	}
	view := ConnectorView{
		ID:              id,
		Version:         version,
		ContractVersion: contractVersion,
		DisplayName:     name,
		Description:     desc.Description,
		Capabilities:    desc.Capabilities,
		ConfigSchema:    desc.ConfigSchema,
		SecretFields:    desc.SecretFields,
		State:           state,
		ArtifactDigest:  digest,
		LastSeenAt:      lastSeen,
	}
	if lastErr != nil {
		view.LastError = *lastErr
	}
	if len(view.ConfigSchema) == 0 {
		view.ConfigSchema = json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	return view
}

// ListConnectors returns the catalog with real health. Read-only for any
// tenant role that may view northbound configuration.
//
//encore:api auth method=GET path=/v1/connectors
func ListConnectors(ctx context.Context) (*ConnectorListResponse, error) {
	data, err := principal()
	if err != nil {
		return nil, err
	}
	if err := requireAction(data, authz.ActionView); err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT id, version, contract_version, endpoint, artifact_digest, state,
		capabilities, last_seen_at, last_error FROM nb_connector_installations ORDER BY id`)
	if err != nil {
		return nil, errUnavailable("connector catalog unavailable")
	}
	defer rows.Close()
	out := &ConnectorListResponse{ContractVersion: ContractVersion, Connectors: []ConnectorView{}}
	for rows.Next() {
		var id, version, contractVersion, endpoint, digest, state string
		var caps []byte
		var lastSeen *time.Time
		var lastErr *string
		if err := rows.Scan(&id, &version, &contractVersion, &endpoint, &digest, &state, &caps, &lastSeen, &lastErr); err != nil {
			return nil, errUnavailable("connector catalog unavailable")
		}
		out.Connectors = append(out.Connectors, toConnectorView(id, version, contractVersion, endpoint, digest, state, caps, lastSeen, lastErr))
	}
	return out, rows.Err()
}

// classifyPluginError turns a plugin call error into the decision the worker
// needs: retry later, or stop and ask the operator to repair the connection.
//
// This exists as a single function on purpose. It used to be inlined at two call
// sites as `var pe *pluginError; if isPluginFailure(err) { kind = pe.kind }`,
// which dereferenced a nil pointer every time a plugin actually failed — the
// first plugin restart took the whole service down with a SIGSEGV. One function,
// one place to get it right, and one place for the test to cover.
func classifyPluginError(err error) pluginErrorKind {
	var pe *pluginError
	if errors.As(err, &pe) && pe != nil {
		return pe.kind
	}
	// Anything that is not a pluginError is treated as "unreachable": the safe
	// direction is to retry, never to declare the connection permanently broken.
	return pluginUnreachable
}
