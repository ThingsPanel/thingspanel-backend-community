package storage

import (
	"bytes"
	"context"
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

// IoTDBHistorySink delivers durable history intents to an IoTDB Table-model
// database through its REST Table API. Each telemetry point is one row keyed by
// (tenant_id, device_id, k, time), so retrying an intent is idempotent.
type IoTDBHistorySink struct {
	BaseURL  string
	User     string
	Password string
	Database string
	Client   *http.Client
}

var iotdbName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

const (
	iotdbInsertChunk   = 500
	iotdbQueryRowLimit = 10000
)

func ValidIoTDBDatabaseName(name string) bool { return iotdbName.MatchString(name) }

func (s *IoTDBHistorySink) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

type iotdbRequest struct {
	Database string `json:"database"`
	SQL      string `json:"sql,omitempty"`
	RowLimit int    `json:"row_limit,omitempty"`
}

type iotdbResponse struct {
	Code        json.RawMessage `json:"code"`
	Message     string          `json:"message"`
	ColumnNames []string        `json:"column_names"`
	DataTypes   []string        `json:"data_types"`
	Values      [][]interface{} `json:"values"`
}

func (s *IoTDBHistorySink) request(ctx context.Context, endpoint string, payload interface{}, out *iotdbResponse) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := strings.TrimRight(s.BaseURL, "/") + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.User, s.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client().Do(req)
	if err != nil {
		return fmt.Errorf("iotdb request failed (outcome unknown): %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("iotdb response unreadable (outcome unknown): %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("iotdb returned HTTP %d with a non-JSON body (outcome unknown)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("iotdb HTTP %d: %s", resp.StatusCode, out.Message)
	}
	if len(out.Code) > 0 && string(out.Code) != "null" {
		var code int
		if err := json.Unmarshal(out.Code, &code); err != nil {
			var text string
			if err := json.Unmarshal(out.Code, &text); err != nil {
				return errors.New("iotdb returned an invalid status code")
			}
			code, err = strconv.Atoi(text)
			if err != nil {
				return errors.New("iotdb returned an invalid status code")
			}
		}
		if code != http.StatusOK {
			return fmt.Errorf("iotdb error %d: %s", code, out.Message)
		}
	}
	return nil
}

func (s *IoTDBHistorySink) Query(ctx context.Context, statement string) ([][]interface{}, error) {
	return s.QueryLimit(ctx, statement, iotdbQueryRowLimit)
}

func (s *IoTDBHistorySink) QueryLimit(ctx context.Context, statement string, rowLimit int) ([][]interface{}, error) {
	if !iotdbName.MatchString(s.Database) {
		return nil, errors.New("iotdb database name must be a plain identifier")
	}
	if rowLimit < 1 {
		return nil, errors.New("iotdb query row limit must be positive")
	}
	var result iotdbResponse
	if err := s.request(ctx, "/rest/table/v1/query", iotdbRequest{Database: s.Database, SQL: statement, RowLimit: rowLimit}, &result); err != nil {
		return nil, err
	}
	return result.Values, nil
}

func (s *IoTDBHistorySink) nonQuery(ctx context.Context, database, statement string) error {
	var result iotdbResponse
	if err := s.request(ctx, "/rest/table/v1/nonQuery", iotdbRequest{Database: database, SQL: statement}, &result); err != nil {
		return err
	}
	return nil
}

// EnsureSchema creates the IoTDB database and the telemetry table idempotently.
func (s *IoTDBHistorySink) EnsureSchema(ctx context.Context) error {
	if !iotdbName.MatchString(s.Database) {
		return errors.New("iotdb database name must be a plain identifier")
	}
	if err := s.nonQuery(ctx, "", "CREATE DATABASE IF NOT EXISTS "+s.Database); err != nil {
		return err
	}
	return s.nonQuery(ctx, s.Database, `CREATE TABLE IF NOT EXISTS telemetry (
		time TIMESTAMP TIME,
		tenant_id STRING TAG,
		device_id STRING TAG,
		k STRING TAG,
		number_v DOUBLE FIELD,
		bool_v BOOLEAN FIELD,
		string_v STRING FIELD
	)`)
}

// DropDatabase removes this sink's database, primarily for isolated test cleanup.
func (s *IoTDBHistorySink) DropDatabase(ctx context.Context) error {
	if !iotdbName.MatchString(s.Database) {
		return errors.New("iotdb database name must be a plain identifier")
	}
	return s.nonQuery(ctx, "", "DROP DATABASE IF EXISTS "+s.Database)
}

func IoTDBQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func iotdbIntentValues(in HistoryIntent) []interface{} {
	var number interface{}
	if in.NumberV != nil && !math.IsNaN(*in.NumberV) && !math.IsInf(*in.NumberV, 0) {
		number = *in.NumberV
	}
	var boolean interface{}
	if in.BoolV != nil {
		boolean = *in.BoolV
	}
	var text interface{}
	if in.StringV != nil {
		text = *in.StringV
	}
	return []interface{}{in.TenantID, in.DeviceID, in.Key, number, boolean, text}
}

// Append writes row-major REST tablets. IoTDB's row identity includes all TAG
// columns and time, which makes retry after an unknown HTTP outcome safe.
func (s *IoTDBHistorySink) Append(ctx context.Context, intents []HistoryIntent) error {
	if !iotdbName.MatchString(s.Database) {
		return errors.New("iotdb database name must be a plain identifier")
	}
	for offset := 0; offset < len(intents); offset += iotdbInsertChunk {
		end := offset + iotdbInsertChunk
		if end > len(intents) {
			end = len(intents)
		}
		chunk := intents[offset:end]
		// A device/key/time can have more than one generation in the pending queue.
		// Keep the last claimed version so one tablet never contains duplicate row keys.
		type rowKey struct {
			tenant, device, key string
			ts                  int64
		}
		positions := make(map[rowKey]int, len(chunk))
		coalesced := make([]HistoryIntent, 0, len(chunk))
		for _, in := range chunk {
			key := rowKey{tenant: in.TenantID, device: in.DeviceID, key: in.Key, ts: in.TS}
			if index, ok := positions[key]; ok {
				coalesced[index] = in
				continue
			}
			positions[key] = len(coalesced)
			coalesced = append(coalesced, in)
		}
		chunk = coalesced
		values := make([][]interface{}, len(chunk))
		timestamps := make([]int64, len(chunk))
		for i, in := range chunk {
			timestamps[i] = in.TS
			values[i] = iotdbIntentValues(in)
		}
		payload := struct {
			Database         string          `json:"database"`
			Table            string          `json:"table"`
			ColumnNames      []string        `json:"column_names"`
			ColumnCategories []string        `json:"column_categories"`
			DataTypes        []string        `json:"data_types"`
			Timestamps       []int64         `json:"timestamps"`
			Values           [][]interface{} `json:"values"`
		}{
			Database: s.Database, Table: "telemetry",
			ColumnNames:      []string{"tenant_id", "device_id", "k", "number_v", "bool_v", "string_v"},
			ColumnCategories: []string{"TAG", "TAG", "TAG", "FIELD", "FIELD", "FIELD"},
			DataTypes:        []string{"STRING", "STRING", "STRING", "DOUBLE", "BOOLEAN", "STRING"},
			Timestamps:       timestamps, Values: values,
		}
		var result iotdbResponse
		if err := s.request(ctx, "/rest/table/v1/insertTablet", payload, &result); err != nil {
			return fmt.Errorf("append IoTDB history intents %d..%d: %w", chunk[0].ID, chunk[len(chunk)-1].ID, err)
		}
	}
	return nil
}

// DeleteDevice removes all points for one device; it is idempotent.
func (s *IoTDBHistorySink) DeleteDevice(ctx context.Context, deviceID string) error {
	if !iotdbName.MatchString(s.Database) {
		return errors.New("iotdb database name must be a plain identifier")
	}
	return s.nonQuery(ctx, s.Database, "DELETE FROM telemetry WHERE device_id="+IoTDBQuote(deviceID))
}

func (s *IoTDBHistorySink) DeleteKey(ctx context.Context, deviceID, key string) error {
	if !iotdbName.MatchString(s.Database) {
		return errors.New("iotdb database name must be a plain identifier")
	}
	return s.nonQuery(ctx, s.Database, "DELETE FROM telemetry WHERE device_id="+IoTDBQuote(deviceID)+" AND k="+IoTDBQuote(key))
}

func (s *IoTDBHistorySink) DeleteBefore(ctx context.Context, timestamp int64) error {
	if !iotdbName.MatchString(s.Database) {
		return errors.New("iotdb database name must be a plain identifier")
	}
	return s.nonQuery(ctx, s.Database, fmt.Sprintf("DELETE FROM telemetry WHERE time<=%d", timestamp))
}
