package store_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"encore.app/marketinstall/app"
	"encore.app/marketinstall/store"
	"encore.app/pkg/ownermigration"
)

func (f relationalFixture) oneParameter(query string) string {
	if f.mysql {
		return strings.ReplaceAll(query, "$1", "?")
	}
	return query
}

func TestRelationalLegacyPlanAdoptionRequiresEveryOriginalStep(t *testing.T) {
	for _, which := range []string{"compatible", "future_step_digest", "missing_future_step", "legacy_exponent", "unknown_encoding", "missing_exact", "unversioned_exact", "tampered_exact"} {
		t.Run(which, func(t *testing.T) {
			forRelationalStores(t, func(t *testing.T, f relationalFixture) {
				op := testOperation(t, 2)
				if which == "legacy_exponent" {
					op.Plan.Steps[0].Payload = json.RawMessage(`{"integer":9007199254740993,"exponent":1e+20}`)
				}
				if _, err := f.store.Create(f.ctx, op); err != nil {
					t.Fatal(err)
				}
				var statement string
				switch which {
				case "compatible", "future_step_digest", "missing_future_step", "legacy_exponent":
					statement = `UPDATE market_install_operations SET plan_encoding=NULL,plan_exact=NULL WHERE id=$1`
				case "unknown_encoding":
					alter := `ALTER TABLE market_install_operations DROP CONSTRAINT market_install_plan_encoding`
					if f.mysql {
						alter = `ALTER TABLE market_install_operations DROP CHECK market_install_plan_encoding`
					}
					if _, err := f.db.ExecContext(f.ctx, alter); err != nil {
						t.Fatal(err)
					}
					statement = `UPDATE market_install_operations SET plan_encoding='unknown-number-v999' WHERE id=$1`
				case "missing_exact":
					statement = `UPDATE market_install_operations SET plan_exact=NULL WHERE id=$1`
				case "unversioned_exact":
					statement = `UPDATE market_install_operations SET plan_encoding=NULL WHERE id=$1`
				case "tampered_exact":
					changed := op.Plan
					changed.Steps = append([]app.Step(nil), op.Plan.Steps...)
					changed.Steps[1].Payload = json.RawMessage(`{"changed":true}`)
					encoded, err := json.Marshal(changed)
					if err != nil {
						t.Fatal(err)
					}
					query := `UPDATE market_install_operations SET plan_exact=$1 WHERE id=$2`
					if f.mysql {
						query = `UPDATE market_install_operations SET plan_exact=? WHERE id=?`
					}
					if _, err := f.db.ExecContext(f.ctx, query, string(encoded), op.ID); err != nil {
						t.Fatal(err)
					}
				}
				if statement != "" {
					if _, err := f.db.ExecContext(f.ctx, f.oneParameter(statement), op.ID); err != nil {
						t.Fatal(err)
					}
				}
				if which == "future_step_digest" {
					if _, err := f.db.ExecContext(f.ctx, f.oneParameter(`UPDATE market_install_steps SET payload_hash='`+strings.Repeat("c", 64)+`' WHERE operation_id=$1 AND ordinal=1`), op.ID); err != nil {
						t.Fatal(err)
					}
				}
				if which == "missing_future_step" {
					if _, err := f.db.ExecContext(f.ctx, f.oneParameter(`DELETE FROM market_install_steps WHERE operation_id=$1 AND ordinal=1`), op.ID); err != nil {
						t.Fatal(err)
					}
				}
				var beforeEncoding sql.NullString
				var beforeExact []byte
				if err := f.db.QueryRowContext(f.ctx, f.oneParameter(`SELECT plan_encoding,plan_exact FROM market_install_operations WHERE id=$1`), op.ID).Scan(&beforeEncoding, &beforeExact); err != nil {
					t.Fatal(err)
				}
				observed, readErr := f.store.Get(f.ctx, op.TenantID, op.SubjectID, op.RequestKey)
				invalidEncoding := which == "unknown_encoding" || which == "missing_exact" || which == "unversioned_exact"
				if invalidEncoding {
					if readErr == nil || errors.Is(readErr, app.ErrNotFound) {
						t.Fatalf("invalid exact encoding became a usable operation or absence: %v", readErr)
					}
				} else if readErr != nil || observed.ID != op.ID {
					t.Fatalf("read-only original-key result disappeared: %#v %v", observed, readErr)
				}
				work, err := f.store.Claim(f.ctx, 60)
				allowed := which == "compatible" || which == "legacy_exponent" && f.mysql
				var encoding sql.NullString
				var state string
				var exact []byte
				var generation int64
				if scanErr := f.db.QueryRowContext(f.ctx, f.oneParameter(`SELECT plan_encoding,plan_exact,state,generation FROM market_install_operations WHERE id=$1`), op.ID).Scan(&encoding, &exact, &state, &generation); scanErr != nil {
					t.Fatal(scanErr)
				}
				if allowed {
					if err != nil || work == nil {
						t.Fatalf("fully verified legacy plan was not adopted: %#v %v", work, err)
					}
					hash, _ := app.StepHash(op.Plan.Steps[0])
					if (!encoding.Valid || encoding.String != store.PlanEncodingV1) || len(exact) == 0 || state != app.Running || generation != 1 || work.PayloadHash != hash {
						t.Fatal("legacy adoption changed original digest or was not atomically claimed")
					}
				} else {
					if err == nil || work != nil {
						t.Fatalf("unverified legacy/exact plan produced executable work: %#v %v", work, err)
					}
					if encoding != beforeEncoding || !bytes.Equal(exact, beforeExact) || state != app.Queued || generation != 0 {
						t.Fatal("failed integrity check changed encoding, state or generation")
					}
				}
			})
		})
	}
}

func TestMySQLAdditiveExactPlanUpgradeFromOriginalVersionTwo(t *testing.T) {
	f := mysqlStore(t, true)
	// The ledger definition is created by the production runner. Rebuild only
	// this private fixture's business tables from the immutable v1/v2 SQL bytes
	// and their actual owner version receipts, then upgrade with the real runner.
	if err := ownermigration.WithMySQLSchemaLock(f.ctx, f.db, func(conn *sql.Conn, database string) error {
		if database != f.database {
			return errors.New("wrong fixture schema")
		}
		for _, statement := range []string{`DROP TABLE market_install_steps`, `DROP TABLE market_install_operations`, `DELETE FROM market_install_schema_migrations`} {
			if _, err := conn.ExecContext(f.ctx, statement); err != nil {
				return err
			}
		}
		for _, name := range []string{"0001_operations.sql", "0002_steps.sql"} {
			source, err := os.ReadFile(filepath.Join("mysql_migrations", name))
			if err != nil {
				return err
			}
			if _, err := conn.ExecContext(f.ctx, string(source)); err != nil {
				return err
			}
			digest := sha256.Sum256(source)
			if _, err := conn.ExecContext(f.ctx, `INSERT INTO market_install_schema_migrations(version,checksum,state,applied_at) VALUES(?,?,'applied',CURRENT_TIMESTAMP(6))`, name, hex.EncodeToString(digest[:])); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op := testOperation(t, 1)
	op.Plan.Steps[0].Payload = json.RawMessage(`{"integer":9007199254740993,"exponent":1e+20}`)
	encoded, err := json.Marshal(op.Plan)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.StepHash(op.Plan.Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	// Seed the original v2 row format; no new business/provider implementation
	// is substituted for the production owner being upgraded and claimed below.
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO market_install_operations(id,tenant_id,subject_id,authorization_ref,request_key,request_digest,artifact_digest,plan,state,bundle_key,bundle_version) VALUES(?,?,?,?,?,?,?,?,'queued',?,?)`, op.ID, op.TenantID, op.SubjectID, op.AuthorizationRef, op.RequestKey, op.RequestDigest, op.ArtifactDigest, string(encoded), op.BundleKey, op.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO market_install_steps(operation_id,ordinal,step_id,idempotency_key,payload_hash,state) VALUES(?,0,?,?,?,'pending')`, op.ID, op.Plan.Steps[0].ID, app.StableStepKey(op.ID, op.Plan.Steps[0]), hash); err != nil {
		t.Fatal(err)
	}
	if err := f.store.VerifyMySQLSchema(f.ctx); err == nil {
		t.Fatal("old schema was accepted as current")
	}
	if err := f.store.MigrateMySQL(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.VerifyMySQLSchema(f.ctx); err != nil {
		t.Fatal(err)
	}
	var encoding sql.NullString
	var exact []byte
	if err := f.db.QueryRowContext(f.ctx, `SELECT plan_encoding,plan_exact FROM market_install_operations WHERE id=?`, op.ID).Scan(&encoding, &exact); err != nil {
		t.Fatal(err)
	}
	if encoding.Valid || exact != nil {
		t.Fatal("schema upgrade adopted an unverified business plan")
	}
	work, err := f.store.Claim(f.ctx, 60)
	if err != nil || work == nil || work.PayloadHash != hash {
		t.Fatalf("original v2 plan could not be safely adopted after upgrade: %#v %v", work, err)
	}
	if err := f.db.QueryRowContext(f.ctx, `SELECT plan_encoding,plan_exact FROM market_install_operations WHERE id=?`, op.ID).Scan(&encoding, &exact); err != nil {
		t.Fatal(err)
	}
	if (!encoding.Valid || encoding.String != store.PlanEncodingV1) || !bytes.Equal(exact, encoded) {
		t.Fatal("MySQL upgrade changed the original exact plan bytes")
	}
}

func TestMySQLSilentMigrationReceiptIsNeverReady(t *testing.T) {
	f := mysqlStore(t, true)
	if _, err := f.db.ExecContext(f.ctx, `UPDATE market_install_schema_migrations SET state='applying',applied_at=NULL WHERE version='0004_claim_ready.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, `CREATE TRIGGER tp_a08_no_receipt BEFORE UPDATE ON market_install_schema_migrations FOR EACH ROW BEGIN SET NEW.state=OLD.state; SET NEW.applied_at=OLD.applied_at; END`); err != nil {
		t.Fatal(err)
	}
	if err := f.store.MigrateMySQL(f.ctx); !errors.Is(err, ownermigration.ErrOutcomeUnknown) {
		t.Fatalf("unpersisted owner migration receipt was acknowledged: %v", err)
	}
	if err := f.store.VerifyMySQLSchema(f.ctx); !errors.Is(err, ownermigration.ErrOutcomeUnknown) {
		t.Fatalf("unpersisted migration was considered ready: %v", err)
	}
	if _, err := f.db.ExecContext(f.ctx, `DROP TRIGGER tp_a08_no_receipt`); err != nil {
		t.Fatal(err)
	}
	if err := f.store.MigrateMySQL(f.ctx); err != nil {
		t.Fatal(err)
	}
}
