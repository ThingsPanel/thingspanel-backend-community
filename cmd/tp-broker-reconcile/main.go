// tp-broker-reconcile validates a bounded, offline event evidence pack. It has
// no broker, database, or model integration.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"project/internal/brokercontract"
)

const (
	profile       = "qos1-durable-event-business-row-v1"
	manifestLimit = 64 << 10
	lineLimit     = 8 << 10
	inputLimit    = 128 << 20
	recordLimit   = 700000
	maxID         = 256
)

var stages = []string{"planned", "sent", "broker_ack", "ingress_seen", "accepted", "terminal", "sideeffects"}

type manifest struct {
	SchemaVersion int               `json:"schema_version"`
	RunID         string            `json:"run_id"`
	Profile       string            `json:"profile"`
	ExpectedCount int               `json:"expected_count"`
	DeadlineNS    int64             `json:"deadline_ns"`
	ClockID       string            `json:"clock_id"`
	SHA256        map[string]string `json:"sha256"`
}

type record struct {
	RunID     string `json:"run_id"`
	EventID   string `json:"event_id"`
	TenantID  string `json:"tenant_id"`
	DeviceID  string `json:"device_id"`
	StreamID  string `json:"stream_id"`
	ClockID   string `json:"clock_id"`
	ElapsedNS int64  `json:"elapsed_ns"`
	ReceiptID string `json:"receipt_id,omitempty"`
	EffectID  string `json:"effect_id,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
}

type event struct {
	byStage    map[string]record
	effects    []record
	effectSeen map[string]struct{}
}

type result struct {
	Status           string   `json:"status"`
	Profile          string   `json:"profile"`
	Expected         int      `json:"expected_count"`
	Records          int      `json:"records"`
	Loss             int      `json:"loss"`
	WrongScope       int      `json:"wrong_scope"`
	Timeout          int      `json:"timeout"`
	TimeOrder        int      `json:"time_order"`
	ReceiptConflicts int      `json:"receipt_conflicts"`
	TerminalFailures int      `json:"terminal_failures"`
	DuplicateEffects int      `json:"duplicate_effects"`
	Examples         []string `json:"examples,omitempty"`
	Error            string   `json:"error,omitempty"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tp-broker-reconcile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "directory containing manifest.json and seven stage files")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *dir == "" {
		fmt.Fprintln(stderr, "usage: tp-broker-reconcile --dir DIR")
		return 64
	}
	r, code := reconcile(*dir)
	b, _ := json.Marshal(r)
	fmt.Fprintln(stdout, string(b))
	return code
}

func reconcile(dir string) (result, int) {
	r := result{Status: "INVALID", Profile: profile}
	bad := func(msg string) (result, int) { r.Error = msg; return r, 2 }
	blocked := func(msg string) (result, int) { r.Status = "BLOCKED"; r.Error = msg; return r, 3 }
	failed := false
	addExample := func(s string) {
		if len(r.Examples) < 100 {
			r.Examples = append(r.Examples, s)
		}
	}

	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return bad("invalid evidence directory")
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	mi, err := os.Lstat(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return blocked("manifest missing")
	}
	if err != nil || !mi.Mode().IsRegular() || mi.Mode()&os.ModeSymlink != 0 || mi.Size() > manifestLimit {
		return bad("invalid manifest file")
	}
	mf, err := os.Open(manifestPath)
	if err != nil {
		return bad("cannot read manifest")
	}
	mb, err := io.ReadAll(io.LimitReader(mf, manifestLimit+1))
	closeErr := mf.Close()
	if err != nil || closeErr != nil || len(mb) > manifestLimit || brokercontract.ValidateUniqueJSON(mb) != nil || !validateManifestShape(mb) {
		return bad("invalid manifest JSON")
	}
	var m manifest
	if decodeStrict(mb, &m) != nil {
		return bad("invalid manifest structure")
	}
	if m.SchemaVersion != 1 || !validID(m.RunID) || m.Profile != profile || m.ExpectedCount < 1 || m.ExpectedCount > 100000 || m.DeadlineNS <= 0 || m.DeadlineNS > int64(60*60*1e9) || !validID(m.ClockID) || len(m.SHA256) != len(stages) {
		return bad("invalid manifest values")
	}
	for _, s := range stages {
		h, ok := m.SHA256[s]
		if !ok || len(h) != 64 {
			return bad("invalid stage hash manifest")
		}
		if _, e := hex.DecodeString(h); e != nil {
			return bad("invalid stage hash manifest")
		}
	}
	for k := range m.SHA256 {
		known := false
		for _, s := range stages {
			if k == s {
				known = true
				break
			}
		}
		if !known {
			return bad("unknown stage hash")
		}
	}
	r.Expected = m.ExpectedCount
	events := make(map[string]*event, m.ExpectedCount)
	var totalBytes int64 = int64(len(mb))
	if totalBytes > inputLimit {
		return bad("evidence input exceeds byte limit")
	}
	totalRecords := 0
	for _, stage := range stages {
		path := filepath.Join(dir, stage+".ndjson")
		st, e := os.Lstat(path)
		if errors.Is(e, os.ErrNotExist) {
			return blocked("required stage file missing")
		}
		if e != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
			return bad("invalid stage file")
		}
		if st.Size() < 0 || st.Size() > inputLimit-totalBytes {
			return bad("evidence input exceeds byte limit")
		}
		f, e := os.Open(path)
		if e != nil {
			return bad("cannot read evidence file")
		}
		h := sha256.New()
		reader := bufio.NewReaderSize(io.TeeReader(f, h), 4096)
		stageRecords := 0
		stageBytes := int64(0)
		for {
			line, readErr := readBoundedLine(reader)
			if errors.Is(readErr, errLineTooLong) {
				f.Close()
				return bad("record exceeds line limit")
			}
			stageBytes += int64(len(line))
			if stageBytes > inputLimit-totalBytes {
				f.Close()
				return bad("evidence input exceeds byte limit")
			}
			if len(line) > 0 {
				line = bytes.TrimSuffix(line, []byte{'\n'})
				line = bytes.TrimSuffix(line, []byte{'\r'})
				if len(bytes.TrimSpace(line)) == 0 {
					f.Close()
					return bad("blank evidence record")
				}
				var rec record
				if brokercontract.ValidateUniqueJSON(line) != nil || !validateRecordShape(line, stage) || decodeStrict(line, &rec) != nil || !validRecord(rec, stage, m) {
					f.Close()
					return bad("invalid evidence record")
				}
				stageRecords++
				totalRecords++
				if totalRecords > recordLimit {
					f.Close()
					return bad("evidence record limit exceeded")
				}
				if events[rec.EventID] == nil {
					events[rec.EventID] = &event{byStage: map[string]record{}, effectSeen: map[string]struct{}{}}
				}
				ev := events[rec.EventID]
				if prior, exists := ev.byStage[stage]; exists {
					if stage == "sideeffects" && prior.EffectID != rec.EffectID {
						if _, exists := ev.effectSeen[rec.EffectID]; exists {
							f.Close()
							return bad("duplicate sideeffect record")
						}
						ev.effectSeen[rec.EffectID] = struct{}{}
						ev.effects = append(ev.effects, rec)
					} else {
						f.Close()
						return bad("duplicate event record within stage")
					}
				} else {
					ev.byStage[stage] = rec
					if stage == "sideeffects" {
						ev.effectSeen[rec.EffectID] = struct{}{}
						ev.effects = append(ev.effects, rec)
					}
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				f.Close()
				return bad("cannot read evidence file")
			}
		}
		if e = f.Close(); e != nil {
			return bad("cannot close evidence file")
		}
		totalBytes += stageBytes
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(got, m.SHA256[stage]) {
			return bad("stage hash mismatch")
		}
		if stageRecords == 0 && stage == "planned" {
			return blocked("planned evidence empty")
		}
	}
	r.Records = totalRecords
	planned := make(map[string]record)
	eventKeys := make([]string, 0, len(events))
	for id := range events {
		eventKeys = append(eventKeys, id)
	}
	sort.Strings(eventKeys)
	for _, id := range eventKeys {
		ev := events[id]
		if p, ok := ev.byStage["planned"]; ok {
			planned[id] = p
		}
	}
	if len(planned) < m.ExpectedCount {
		r.Loss += m.ExpectedCount - len(planned)
		failed = true
		addExample("planned evidence count below expected")
	}
	if len(planned) > m.ExpectedCount {
		failed = true
		addExample("planned evidence count exceeds expected")
	}
	if len(planned) == 0 {
		return blocked("no planned events")
	}
	effectIDs := map[string]string{}
	receiptIDs := map[string]string{}
	for _, id := range eventKeys {
		ev := events[id]
		p, isPlanned := planned[id]
		if !isPlanned {
			failed = true
			addExample(example("unplanned_event", id))
			continue
		}
		missing := false
		for _, stage := range stages {
			if _, ok := ev.byStage[stage]; !ok {
				missing = true
				if stage != "planned" {
					r.Loss++
				}
				addExample(example("missing_"+stage, id))
				break
			}
		}
		if missing {
			failed = true
			continue
		}
		for _, stage := range stages[1:] {
			v := ev.byStage[stage]
			if !sameScope(p, v) {
				r.WrongScope++
				failed = true
				addExample(example("wrong_scope_"+stage, id))
			}
			if v.ElapsedNS < p.ElapsedNS {
				r.TimeOrder++
				failed = true
				addExample(example("time_before_planned_"+stage, id))
			}
		}
		terminal := ev.byStage["terminal"]
		if terminal.Outcome == "failure" {
			r.TerminalFailures++
			failed = true
			addExample(example("terminal_failure", id))
		}
		if terminal.ElapsedNS-p.ElapsedNS > m.DeadlineNS {
			r.Timeout++
			failed = true
			addExample(example("deadline_exceeded", id))
		}
		accepted := ev.byStage["accepted"]
		if accepted.ReceiptID == "" || terminal.ReceiptID == "" || accepted.ReceiptID != terminal.ReceiptID {
			failed = true
			addExample(example("receipt_identity_mismatch", id))
		} else if prior, ok := receiptIDs[accepted.ReceiptID]; ok && prior != id {
			r.ReceiptConflicts++
			failed = true
			addExample(example("receipt_reused", id))
		} else {
			receiptIDs[accepted.ReceiptID] = id
		}
		se := ev.byStage["sideeffects"]
		if se.EffectID == "" {
			failed = true
			addExample(example("business_effect_missing", id))
		}
		for _, effect := range ev.effects {
			if prior, ok := effectIDs[effect.EffectID]; ok && prior != id {
				r.DuplicateEffects++
				failed = true
				addExample(example("effect_id_reused", id))
			} else {
				effectIDs[effect.EffectID] = id
			}
		}
		if len(ev.effects) > 1 {
			r.DuplicateEffects += len(ev.effects) - 1
			failed = true
			addExample(example("multiple_business_effects", id))
		}
	}
	if failed {
		r.Status = "FAIL"
		return r, 1
	}
	r.Status = "PASS"
	return r, 0
}

func validateManifestShape(raw []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	keys := []string{"schema_version", "run_id", "profile", "expected_count", "deadline_ns", "clock_id", "sha256"}
	if len(fields) != len(keys) {
		return false
	}
	for _, key := range keys {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}

func validateRecordShape(raw []byte, stage string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	required := []string{"run_id", "event_id", "tenant_id", "device_id", "stream_id", "clock_id", "elapsed_ns"}
	allowed := map[string]bool{}
	for _, k := range required {
		allowed[k] = true
		if _, ok := fields[k]; !ok {
			return false
		}
	}
	switch stage {
	case "accepted":
		allowed["receipt_id"] = true
	case "terminal":
		allowed["receipt_id"] = true
		allowed["outcome"] = true
	case "sideeffects":
		allowed["effect_id"] = true
	}
	for k, v := range fields {
		if !allowed[k] || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return false
		}
	}
	return true
}

func example(reason, eventID string) string {
	sum := sha256.Sum256([]byte(eventID))
	return reason + ":event#" + hex.EncodeToString(sum[:4])
}

var errLineTooLong = errors.New("record exceeds line limit")

func readBoundedLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(line)+len(part) > lineLimit {
			return nil, errLineTooLong
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err == io.EOF && len(line) > 0 {
			return line, io.EOF
		}
		return line, err
	}
}

func decodeStrict(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func validID(s string) bool { return len(s) > 0 && len(s) <= maxID && strings.TrimSpace(s) == s }
func validRecord(v record, stage string, m manifest) bool {
	if !validID(v.RunID) || v.RunID != m.RunID || !validID(v.EventID) || !validID(v.TenantID) || !validID(v.DeviceID) || !validID(v.StreamID) || !validID(v.ClockID) || v.ClockID != m.ClockID || v.ElapsedNS < 0 {
		return false
	}
	switch stage {
	case "planned", "sent", "broker_ack", "ingress_seen":
		return v.ReceiptID == "" && v.EffectID == "" && v.Outcome == ""
	case "accepted":
		return validID(v.ReceiptID) && v.EffectID == "" && v.Outcome == ""
	case "terminal":
		return validID(v.ReceiptID) && v.EffectID == "" && (v.Outcome == "success" || v.Outcome == "failure")
	case "sideeffects":
		return v.ReceiptID == "" && validID(v.EffectID) && v.Outcome == ""
	default:
		return false
	}
}
func sameScope(a, b record) bool {
	return a.RunID == b.RunID && a.EventID == b.EventID && a.TenantID == b.TenantID && a.DeviceID == b.DeviceID && a.StreamID == b.StreamID && a.ClockID == b.ClockID
}

func usage(w io.Writer) { fmt.Fprintln(w, "Usage: tp-broker-reconcile --dir DIR") }
