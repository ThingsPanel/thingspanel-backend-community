package storage

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TDengineHistorySink delivers replayed history intents to TDengine over its REST
// interface (taosAdapter), so no native driver is linked.
//
// Layout: one super table with tags (tenant_id, device_id, k) and one child table
// per (tenant, device, key), named from a digest of those three values. A row is
// identified by (child table, ts); TDengine replaces a row written again with the
// same timestamp, so replaying an intent is idempotent. A new generation of the
// same identity replaces the earlier value, which is the intended effect of a
// delete-and-recreate. Non-finite numbers are stored as NULL because TDengine
// cannot represent them; that is the one lossy case.
type TDengineHistorySink struct {
	BaseURL  string // for example http://127.0.0.1:6041
	User     string
	Password string
	Database string
	Client   *http.Client
}

var tdengineName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

const (
	tdengineMaxID     = 64
	tdengineMaxKey    = 128
	tdengineMaxString = 1024
	tdengineChunk     = 200
)

func (s *TDengineHistorySink) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s *TDengineHistorySink) exec(ctx context.Context, statement string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.BaseURL, "/")+"/rest/sql", bytes.NewBufferString(statement))
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.User, s.Password)
	resp, err := s.client().Do(req)
	if err != nil {
		return fmt.Errorf("tdengine request failed (outcome unknown): %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("tdengine response unreadable (outcome unknown): %w", err)
	}
	var result struct {
		Code int    `json:"code"`
		Desc string `json:"desc"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("tdengine returned HTTP %d with a non-JSON body (outcome unknown)", resp.StatusCode)
	}
	if result.Code != 0 {
		return fmt.Errorf("tdengine error %d: %s", result.Code, result.Desc)
	}
	return nil
}

// EnsureSchema creates the database and super table when they do not exist.
func (s *TDengineHistorySink) EnsureSchema(ctx context.Context) error {
	if !tdengineName.MatchString(s.Database) {
		return errors.New("tdengine database name must be a plain identifier")
	}
	if err := s.exec(ctx, "CREATE DATABASE IF NOT EXISTS "+s.Database+" PRECISION 'ms' KEEP 36500d"); err != nil {
		return err
	}
	return s.exec(ctx, fmt.Sprintf("CREATE STABLE IF NOT EXISTS %s.telemetry (ts TIMESTAMP, number_v DOUBLE, bool_v BOOL, string_v NCHAR(%d)) TAGS (tenant_id NCHAR(%d), device_id NCHAR(%d), k NCHAR(%d))",
		s.Database, tdengineMaxString, tdengineMaxID, tdengineMaxID, tdengineMaxKey))
}

func tdengineQuote(v string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

func tdengineTable(tenant, device, key string) string {
	sum := sha1.Sum([]byte(tenant + "\x00" + device + "\x00" + key))
	return "t_" + hex.EncodeToString(sum[:])
}

func tdengineValue(in HistoryIntent) (number, boolean, text string) {
	number, boolean, text = "NULL", "NULL", "NULL"
	if in.NumberV != nil && !math.IsNaN(*in.NumberV) && !math.IsInf(*in.NumberV, 0) {
		number = strconv.FormatFloat(*in.NumberV, 'g', -1, 64)
	}
	if in.BoolV != nil {
		boolean = strconv.FormatBool(*in.BoolV)
	}
	if in.StringV != nil {
		text = tdengineQuote(*in.StringV)
	}
	return
}

func validateTDengineIntent(in HistoryIntent) error {
	switch {
	case len([]rune(in.TenantID)) > tdengineMaxID, len([]rune(in.DeviceID)) > tdengineMaxID:
		return fmt.Errorf("tenant or device id exceeds %d characters", tdengineMaxID)
	case len([]rune(in.Key)) > tdengineMaxKey:
		return fmt.Errorf("key exceeds %d characters", tdengineMaxKey)
	case in.StringV != nil && len([]rune(*in.StringV)) > tdengineMaxString:
		return fmt.Errorf("string value exceeds %d characters", tdengineMaxString)
	}
	return nil
}

// Append satisfies HistorySink. Every intent is validated before anything is sent.
func (s *TDengineHistorySink) Append(ctx context.Context, intents []HistoryIntent) error {
	if !tdengineName.MatchString(s.Database) {
		return errors.New("tdengine database name must be a plain identifier")
	}
	for _, in := range intents {
		if err := validateTDengineIntent(in); err != nil {
			return fmt.Errorf("history intent %d cannot be stored in tdengine: %w", in.ID, err)
		}
	}
	for offset := 0; offset < len(intents); offset += tdengineChunk {
		end := offset + tdengineChunk
		if end > len(intents) {
			end = len(intents)
		}
		// Group rows per child table so each table's USING clause appears once.
		order := []string{}
		groups := map[string]*strings.Builder{}
		for _, in := range intents[offset:end] {
			name := tdengineTable(in.TenantID, in.DeviceID, in.Key)
			b, ok := groups[name]
			if !ok {
				b = &strings.Builder{}
				fmt.Fprintf(b, "%s.%s USING %s.telemetry TAGS (%s,%s,%s) VALUES ", s.Database, name, s.Database,
					tdengineQuote(in.TenantID), tdengineQuote(in.DeviceID), tdengineQuote(in.Key))
				groups[name] = b
				order = append(order, name)
			}
			number, boolean, text := tdengineValue(in)
			fmt.Fprintf(b, "(%d,%s,%s,%s) ", in.TS, number, boolean, text)
		}
		var statement strings.Builder
		statement.WriteString("INSERT INTO ")
		for _, name := range order {
			statement.WriteString(groups[name].String())
		}
		if err := s.exec(ctx, statement.String()); err != nil {
			return err
		}
	}
	return nil
}
