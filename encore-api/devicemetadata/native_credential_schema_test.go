package devicemetadata

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"encore.dev/storage/sqldb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDeviceCredentialsSchemaConstraints(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	tenantA, tenantB := testTenantID(), testTenantID()
	deviceA := insertCredentialSchemaDevice(t, ctx, tx, tenantA)
	otherDeviceA := insertCredentialSchemaDevice(t, ctx, tx, tenantA)
	deviceB := insertCredentialSchemaDevice(t, ctx, tx, tenantB)
	// Deliberately reuse a UUID across the device and template tables so the
	// rotation FK fixture proves owner kind/purpose are part of the scope.
	templateA, templateB := deviceA, uuid.NewString()
	mustInsertRevisionSchemaTemplate(t, ctx, tx, revisionSchemaTemplate{id: templateA, tenantID: tenantA, name: "credential-schema-a-" + templateA})
	mustInsertRevisionSchemaTemplate(t, ctx, tx, revisionSchemaTemplate{id: templateB, tenantID: tenantB, name: "credential-schema-b-" + templateB})
	opA := insertMetadataOperationFixture(t, ctx, tx, tenantA, "credential_change")
	opB := insertMetadataOperationFixture(t, ctx, tx, tenantB, "credential_change")

	base := credentialSchemaRow{
		id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA,
		purpose: "device_access", kind: "username_password", mode: "encrypted", status: "pending",
		publicIdentifier: "shared-identifier", ciphertext: make([]byte, 17), keyID: "k1", cipherSuite: "AES-256-GCM", nonce: make([]byte, 12),
		fingerprintKeyID: "k1", fingerprint: make([]byte, 32),
	}
	insertCredentialSchemaRow(t, ctx, tx, "valid encrypted pending device credential", base)
	active := credentialSchemaRow{
		id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA,
		purpose: "device_access", kind: "token", mode: "verifier", status: "active", verifier: "scrypt$v=1$fixture",
		publicIdentifier: "shared-identifier", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), activatedAt: timePointer(time.Now().UTC().Add(time.Minute)),
	}
	insertCredentialSchemaRow(t, ctx, tx, "valid active verifier credential", active)
	certificate := credentialSchemaRow{
		id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA,
		purpose: "device_access", kind: "x509", mode: "public_certificate", status: "revoked",
		publicIdentifier: "shared-identifier", fingerprint: make([]byte, 32),
		publicCertificate: "-----BEGIN CERTIFICATE-----\npublic-only\n-----END CERTIFICATE-----",
		revokedAt:         timePointer(time.Now().UTC().Add(time.Minute)), revokedBy: "credential-schema-test", revocationReason: "fixture rotation",
	}
	insertCredentialSchemaRow(t, ctx, tx, "valid revoked public certificate", certificate)
	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "revoked cannot be reactivated", query: `UPDATE device_credentials SET status='active',revoked_at=NULL,revoked_by=NULL,revocation_reason=NULL WHERE id=$1`},
		{name: "revocation evidence immutable", query: `UPDATE device_credentials SET revocation_reason='changed reason' WHERE id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectCredentialSchemaError(t, ctx, tx, tc.name, "23514", func() error {
				_, err := tx.Exec(ctx, tc.query, certificate.id)
				return err
			})
		})
	}
	templateCredential := credentialSchemaRow{
		id: uuid.NewString(), tenantID: tenantA, templateID: templateA,
		purpose: "template_enrollment", kind: "token", mode: "verifier", status: "active", verifier: "scrypt$v=1$fixture",
		publicIdentifier: "shared-identifier", fingerprintKeyID: "k1", fingerprint: make([]byte, 32),
		activatedAt: timePointer(time.Now().UTC().Add(time.Minute)),
	}
	insertCredentialSchemaRow(t, ctx, tx, "valid template enrollment token", templateCredential)

	rotated := base
	rotated.id = uuid.NewString()
	rotated.rotationOperationID = opA
	rotated.supersedesCredentialID = base.id
	insertCredentialSchemaRow(t, ctx, tx, "same-owner rotation", rotated)

	// public identifiers are intentionally not globally unique, including within
	// one tenant: provider-specific identifiers may legitimately collide.
	otherTenantDuplicate := active
	otherTenantDuplicate.id = uuid.NewString()
	otherTenantDuplicate.tenantID = tenantB
	otherTenantDuplicate.deviceID = deviceB
	insertCredentialSchemaRow(t, ctx, tx, "duplicate identifier in another tenant", otherTenantDuplicate)
	otherOwnerCredential := credentialSchemaRow{
		id: uuid.NewString(), tenantID: tenantA, deviceID: otherDeviceA,
		purpose: "device_access", kind: "token", mode: "verifier", status: "pending", verifier: "scrypt$v=1$fixture",
		fingerprintKeyID: "k1", fingerprint: make([]byte, 32),
	}
	insertCredentialSchemaRow(t, ctx, tx, "credential owned by another device in the same tenant", otherOwnerCredential)

	for _, tc := range []struct {
		name  string
		query string
		args  []any
	}{
		{name: "tenant immutable", query: `UPDATE device_credentials SET tenant_id=$2 WHERE id=$1`, args: []any{base.id, tenantB}},
		{name: "owner immutable", query: `UPDATE device_credentials SET device_id=$2 WHERE id=$1`, args: []any{base.id, otherDeviceA}},
		{name: "purpose immutable", query: `UPDATE device_credentials SET purpose='template_enrollment' WHERE id=$1`, args: []any{base.id}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectCredentialSchemaError(t, ctx, tx, tc.name, "23514", func() error {
				_, err := tx.Exec(ctx, tc.query, tc.args...)
				return err
			})
		})
	}

	for _, tc := range []struct {
		name  string
		state string
		row   credentialSchemaRow
	}{
		{name: "owner xor missing", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 32)}},
		{name: "both owners set", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, templateID: templateA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 32)}},
		{name: "purpose mismatches owner", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "template_enrollment", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 32)}},
		{name: "cross-tenant device", state: "23503", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceB, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 32)}},
		{name: "cross-tenant template", state: "23503", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, templateID: templateB, purpose: "template_enrollment", kind: "token", mode: "verifier", status: "active", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), activatedAt: timePointer(time.Now().UTC())}},
		{name: "encrypted missing nonce", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "encrypted", status: "pending", ciphertext: make([]byte, 17), keyID: "k1", cipherSuite: "AES-256-GCM"}},
		{name: "encrypted material exceeds limit", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "encrypted", status: "pending", ciphertext: make([]byte, 8209), keyID: "k1", cipherSuite: "AES-256-GCM", nonce: make([]byte, 12)}},
		{name: "verifier exceeds limit", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", verifier: strings.Repeat("v", 8193), fingerprintKeyID: "k1", fingerprint: make([]byte, 32)}},
		{name: "fingerprint must be 32 bytes", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 31)}},
		{name: "verifier has ciphertext", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", ciphertext: make([]byte, 16), fingerprintKeyID: "k1", fingerprint: make([]byte, 32)}},
		{name: "x509 cannot store encrypted private material", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "x509", mode: "encrypted", status: "pending", ciphertext: make([]byte, 16), keyID: "k1", cipherSuite: "AES-256-GCM", nonce: make([]byte, 12)}},
		{name: "public certificate cannot contain private key", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "x509", mode: "public_certificate", status: "pending", publicCertificate: "-----BEGIN PRIVATE KEY-----not-public-----END PRIVATE KEY-----"}},
		{name: "public certificate length limit", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "x509", mode: "public_certificate", status: "pending", publicCertificate: strings.Repeat("x", 32769)}},
		{name: "public identifier length limit", state: "22001", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", publicIdentifier: strings.Repeat("x", 257)}},
		{name: "revocation reason length limit", state: "22001", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "revoked", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), revokedAt: timePointer(time.Now().UTC()), revokedBy: "schema-test", revocationReason: strings.Repeat("r", 513)}},
		{name: "template enrollment only token", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, templateID: templateA, purpose: "template_enrollment", kind: "username_password", mode: "verifier", status: "active", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), activatedAt: timePointer(time.Now().UTC())}},
		{name: "template enrollment cannot be pending", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, templateID: templateA, purpose: "template_enrollment", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 32)}},
		{name: "active needs activation time", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "active", fingerprintKeyID: "k1", fingerprint: make([]byte, 32)}},
		{name: "expiry must follow activation", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "active", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), activatedAt: timePointer(time.Now().UTC().Add(time.Hour)), expiresAt: timePointer(time.Now().UTC().Add(time.Minute))}},
		{name: "pending cannot have activation time", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), activatedAt: timePointer(time.Now().UTC())}},
		{name: "revoked needs revoke time", state: "23514", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "revoked", fingerprintKeyID: "k1", fingerprint: make([]byte, 32)}},
		{name: "rotation operation tenant scoped", state: "23503", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), rotationOperationID: opB, supersedesCredentialID: base.id}},
		{name: "supersedes must have same owner", state: "23503", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), rotationOperationID: opA, supersedesCredentialID: otherOwnerCredential.id}},
		{name: "same uuid but different owner kind and purpose", state: "23503", row: credentialSchemaRow{id: uuid.NewString(), tenantID: tenantA, deviceID: deviceA, purpose: "device_access", kind: "token", mode: "verifier", status: "pending", fingerprintKeyID: "k1", fingerprint: make([]byte, 32), rotationOperationID: opA, supersedesCredentialID: templateCredential.id}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectCredentialSchemaError(t, ctx, tx, tc.name, tc.state, func() error {
				return execCredentialSchemaRow(ctx, tx, tc.row)
			})
		})
	}
}

type credentialSchemaRow struct {
	id, tenantID, deviceID, templateID string
	purpose, kind, mode, status        string
	publicIdentifier, verifier         string
	ciphertext                         []byte
	keyID, cipherSuite                 string
	nonce                              []byte
	fingerprintKeyID                   string
	fingerprint                        []byte
	publicCertificate                  string
	createdBy, updatedBy               string
	revokedBy, revocationReason        string
	rotationOperationID                string
	supersedesCredentialID             string
	activatedAt, revokedAt             *time.Time
	expiresAt                          *time.Time
}

func insertCredentialSchemaDevice(t *testing.T, ctx context.Context, tx *sqldb.Tx, tenantID string) string {
	t.Helper()
	row := validDeviceIdentitySchemaRow(tenantID)
	row.deviceNumber = deviceIdentityString("credential-device-" + uuid.NewString())
	row.name = deviceIdentityString("credential device")
	row.deviceKind = deviceIdentityString("standalone")
	row.activationState = deviceIdentityString("inactive")
	row.activationTimeSource = deviceIdentityString("not_activated")
	if err := insertDeviceIdentitySchemaRow(ctx, tx, row); err != nil {
		t.Fatalf("insert credential device fixture: %v", err)
	}
	return row.id
}

func insertCredentialSchemaRow(t *testing.T, ctx context.Context, tx *sqldb.Tx, label string, row credentialSchemaRow) {
	t.Helper()
	if err := execCredentialSchemaRow(ctx, tx, row); err != nil {
		t.Fatalf("insert %s: %v", label, err)
	}
}

func execCredentialSchemaRow(ctx context.Context, tx *sqldb.Tx, row credentialSchemaRow) error {
	if len(row.fingerprint) == 0 {
		row.fingerprint = make([]byte, 32)
	}
	if row.fingerprintKeyID == "" && !(row.kind == "x509" && row.mode == "public_certificate") {
		row.fingerprintKeyID = "k1"
	}
	if row.mode == "verifier" && row.verifier == "" {
		row.verifier = "scrypt$v=1$schema-fixture"
	}
	if row.createdBy == "" {
		row.createdBy = "credential-schema-test"
	}
	if row.updatedBy == "" {
		row.updatedBy = "credential-schema-test"
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO device_credentials(
			id,tenant_id,device_id,template_id,purpose,kind,protection_mode,status,public_identifier,
			verifier,fingerprint_key_id,fingerprint,ciphertext,key_id,cipher_suite,nonce,public_certificate,
			rotation_operation_id,supersedes_credential_id,created_by,updated_by,activated_at,revoked_at,revoked_by,revocation_reason,expires_at
		) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,NULLIF($9,''),
			NULLIF($10,''),NULLIF($11,''),$12::bytea,$13::bytea,NULLIF($14,''),NULLIF($15,''),$16::bytea,NULLIF($17,''),
			NULLIF($18,'')::uuid,NULLIF($19,'')::uuid,$20,$21,$22,$23,NULLIF($24,''),NULLIF($25,''),$26)
	`, row.id, row.tenantID, row.deviceID, row.templateID, row.purpose, row.kind, row.mode, row.status,
		row.publicIdentifier, row.verifier, row.fingerprintKeyID, row.fingerprint, row.ciphertext,
		row.keyID, row.cipherSuite, row.nonce, row.publicCertificate, row.rotationOperationID,
		row.supersedesCredentialID, row.createdBy, row.updatedBy, row.activatedAt, row.revokedAt,
		row.revokedBy, row.revocationReason, row.expiresAt)
	return err
}

func expectCredentialSchemaError(t *testing.T, ctx context.Context, tx *sqldb.Tx, name, wantSQLState string, operation func() error) {
	t.Helper()
	savepoint := "credential_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("create savepoint %s: %v", name, err)
	}
	err := operation()
	if err == nil {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("invalid credential %s was accepted", name)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != wantSQLState {
		_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint)
		t.Fatalf("credential %s SQLSTATE=%v, want %s", name, sqlState(err), wantSQLState)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("rollback savepoint %s: %v", name, err)
	}
	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("release savepoint %s: %v", name, err)
	}
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return "<non-postgres>"
}

func timePointer(value time.Time) *time.Time { return &value }
