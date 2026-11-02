package store

import (
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestDialectBindsRepeatedAndReorderedParameters(t *testing.T) {
	ledger := &Store{dialect: MySQL}
	query, args, err := ledger.bind(`UPDATE t SET b=$2,c=$3 WHERE a=$1 OR d=$2`, []any{"first", "second", 3})
	if err != nil || query != `UPDATE t SET b=?,c=? WHERE a=? OR d=?` || !reflect.DeepEqual(args, []any{"second", 3, "first", "second"}) {
		t.Fatalf("invalid binding: %s %#v %v", query, args, err)
	}
	if _, _, err := ledger.bind(`SELECT $3`, []any{1}); err == nil {
		t.Fatal("missing internal SQL argument accepted")
	}
	if _, _, err := (&Store{dialect: Dialect(255)}).bind(`SELECT 1`, nil); err == nil {
		t.Fatal("unknown dialect accepted")
	}
	pg, args, err := New(nil).bind(`SELECT $2,$1`, []any{1, 2})
	if err != nil || pg != `SELECT $2,$1` || !reflect.DeepEqual(args, []any{1, 2}) {
		t.Fatal("PG binding was changed")
	}
}

func TestMySQLConfigurationPinsRequiredConnectionSemantics(t *testing.T) {
	original := mysql.NewConfig()
	original.DBName = "fixture"
	original.MultiStatements = true
	original.InterpolateParams = true
	original.AllowAllFiles = true
	original.Params = map[string]string{"time_zone": "'+09:00'", "sql_mode": "''"}
	config, err := mysqlConfiguration(original)
	if err != nil {
		t.Fatal(err)
	}
	if !config.ClientFoundRows || !config.ParseTime || config.MultiStatements || config.InterpolateParams || config.AllowAllFiles || config.Params["time_zone"] != "'+00:00'" || !strings.Contains(config.Params["sql_mode"], "STRICT_ALL_TABLES") {
		t.Fatal("unsafe connection behavior remained enabled")
	}
	if original.Params["time_zone"] != "'+09:00'" || !original.MultiStatements {
		t.Fatal("configuration mutated the caller's original")
	}
	if _, err := mysqlConfiguration(mysql.NewConfig()); err == nil {
		t.Fatal("implicit schema was accepted")
	}
}

func TestMySQLSupportedCapabilitiesAreExplicit(t *testing.T) {
	for _, version := range []string{"8.0.16", "8.0.36", "8.4.0", "8.4.6-commercial"} {
		if !supportedMySQLVersion(version) {
			t.Fatalf("supported version rejected: %s", version)
		}
	}
	for _, version := range []string{"5.7.44", "8.0.15", "10.11.0-MariaDB", "8.0.36-MariaDB", "9.0.0", "8.0", "not-a-version", "8.-1.20"} {
		if supportedMySQLVersion(version) {
			t.Fatalf("unsupported version accepted: %s", version)
		}
	}
}

func TestMySQLSchemaCheckComparisonPreservesLiteralIdentity(t *testing.T) {
	expected := `state in ('queued','running')`
	actual := "(`state` IN (_binary'queued',_binary'running'))"
	if mysqlCheckSignature(expected) != mysqlCheckSignature(actual) {
		t.Fatal("equivalent MySQL CHECK rendering was rejected")
	}
	for _, changed := range []string{`state in ('QUEUED','running')`, `state in ('queue d','running')`, `state in ('queued ','running')`, `state in ('queued','running','completed')`, `state not in ('queued','running')`} {
		if mysqlCheckSignature(expected) == mysqlCheckSignature(changed) {
			t.Fatalf("weakened or altered literal constraint was accepted: %s", changed)
		}
	}
}

func TestMySQLSchemaCheckAcceptsObserved84MetadataWithoutWeakeningLiterals(t *testing.T) {
	raw, err := hex.DecodeString("286073746174656020696e20285f757466386d62345c276170706c79696e675c272c5f757466386d62345c276170706c6965645c272929")
	if err != nil {
		t.Fatal(err)
	}
	expected := `state in ('applying','applied')`
	if mysqlCheckSignature(string(raw)) != mysqlCheckSignature(expected) {
		t.Fatalf("observed MySQL 8.4.13 metadata was not recognized: %s", raw)
	}
	for _, changed := range []string{
		strings.Replace(string(raw), "applying", "APPLYING", 1),
		strings.Replace(string(raw), "applying", "applying ", 1),
		strings.Replace(string(raw), "applying", "apply ing", 1),
		strings.Replace(string(raw), "applying", "apply\\ing", 1),
		string(raw) + " or 1=1",
	} {
		if mysqlCheckSignature(changed) == mysqlCheckSignature(expected) {
			t.Fatalf("changed literal or condition was accepted: %s", changed)
		}
	}
	if mysqlCheckSignature(`octet_length(request_digest)=64`) != mysqlCheckSignature("(length(`request_digest`)=64)") {
		t.Fatal("known byte-length function synonym was rejected")
	}
	if mysqlCheckSignature(`char_length(request_digest)=64`) == mysqlCheckSignature(`octet_length(request_digest)=64`) {
		t.Fatal("different function was accepted")
	}
}
