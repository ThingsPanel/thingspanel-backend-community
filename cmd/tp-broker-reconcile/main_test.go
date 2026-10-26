package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type pack struct {
	dir    string
	m      manifest
	stages map[string][]record
}

func makePack(t *testing.T, n int) *pack {
	t.Helper()
	p := &pack{dir: t.TempDir(), m: manifest{SchemaVersion: 1, RunID: "run-1", Profile: profile, ExpectedCount: n, DeadlineNS: 1_000_000_000, ClockID: "clock-shared", SHA256: map[string]string{}}, stages: map[string][]record{}}
	for _, stage := range stages {
		p.stages[stage] = nil
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("event-%06d", i)
		base := record{RunID: p.m.RunID, EventID: id, TenantID: fmt.Sprintf("tenant-%d", i%2), DeviceID: fmt.Sprintf("device-%02d", i%10), StreamID: fmt.Sprintf("stream-%02d", i%10), ClockID: p.m.ClockID, ElapsedNS: int64(i * 10)}
		for _, stage := range stages {
			v := base
			switch stage {
			case "accepted":
				v.ReceiptID = "receipt-" + id
			case "terminal":
				v.ReceiptID = "receipt-" + id
				v.Outcome = "success"
			case "sideeffects":
				v.EffectID = "effect-" + id
			}
			p.stages[stage] = append(p.stages[stage], v)
		}
	}
	p.write(t)
	return p
}
func (p *pack) write(t *testing.T) {
	t.Helper()
	for _, stage := range stages {
		var b bytes.Buffer
		for _, v := range p.stages[stage] {
			raw, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			b.Write(raw)
			b.WriteByte('\n')
		}
		raw := b.Bytes()
		sum := sha256.Sum256(raw)
		p.m.SHA256[stage] = hex.EncodeToString(sum[:])
		if e := os.WriteFile(filepath.Join(p.dir, stage+".ndjson"), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	mb, e := json.Marshal(p.m)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(p.dir, "manifest.json"), mb, 0600); e != nil {
		t.Fatal(e)
	}
}
func (p *pack) set(stage string, values []record, t *testing.T) { p.stages[stage] = values; p.write(t) }
func invoke(t *testing.T, dir string) (result, int) {
	t.Helper()
	var out, errout bytes.Buffer
	code := run([]string{"--dir", dir}, &out, &errout)
	var got result
	if e := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &got); e != nil {
		t.Fatalf("invalid output JSON (%d): %s", code, out.String())
	}
	if strings.Contains(out.String(), "payload") || strings.Contains(out.String(), "token") {
		t.Fatal("output exposed forbidden evidence content")
	}
	return got, code
}
func TestReconcileExact1000EventPack(t *testing.T) {
	p := makePack(t, 1000)
	got, code := invoke(t, p.dir)
	if code != 0 || got.Status != "PASS" || got.Expected != 1000 || got.Records != 7000 {
		t.Fatalf("got=%+v code=%d", got, code)
	}
}

func TestReconcileDetectsOneHundredAtEachGap(t *testing.T) {
	t.Run("loss", func(t *testing.T) {
		p := makePack(t, 1000)
		p.set("accepted", p.stages["accepted"][:900], t)
		got, c := invoke(t, p.dir)
		if c != 1 || got.Status != "FAIL" || got.Loss != 100 {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("duplicate_effects", func(t *testing.T) {
		p := makePack(t, 1000)
		xs := append([]record(nil), p.stages["sideeffects"]...)
		for i := 0; i < 100; i++ {
			v := xs[i]
			v.EffectID = fmt.Sprintf("effect-copy-%d", i)
			xs = append(xs, v)
		}
		p.set("sideeffects", xs, t)
		got, c := invoke(t, p.dir)
		if c != 1 || got.Status != "FAIL" || got.DuplicateEffects != 100 {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("wrong_scope", func(t *testing.T) {
		p := makePack(t, 1000)
		xs := append([]record(nil), p.stages["terminal"]...)
		for i := 0; i < 100; i++ {
			xs[i].TenantID = "wrong-tenant"
		}
		p.set("terminal", xs, t)
		got, c := invoke(t, p.dir)
		if c != 1 || got.Status != "FAIL" || got.WrongScope != 100 {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		p := makePack(t, 1000)
		xs := append([]record(nil), p.stages["terminal"]...)
		for i := 0; i < 100; i++ {
			xs[i].ElapsedNS += p.m.DeadlineNS + 1
		}
		p.set("terminal", xs, t)
		got, c := invoke(t, p.dir)
		if c != 1 || got.Status != "FAIL" || got.Timeout != 100 {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
}

func TestReconcileCountCompensationStillFails(t *testing.T) {
	p := makePack(t, 10)
	xs := append([]record(nil), p.stages["accepted"][:9]...)
	extra := p.stages["accepted"][9]
	extra.EventID = "unplanned-extra"
	extra.ReceiptID = "receipt-extra"
	xs = append(xs, extra)
	p.set("accepted", xs, t)
	got, c := invoke(t, p.dir)
	if c != 1 || got.Status != "FAIL" || len(got.Examples) == 0 {
		t.Fatalf("got=%+v code=%d", got, c)
	}
}

func TestReconcileOrderingReceiptAndTerminalOutcome(t *testing.T) {
	t.Run("stage before planned fails", func(t *testing.T) {
		p := makePack(t, 1)
		for _, stage := range stages {
			for i := range p.stages[stage] {
				if stage == "planned" {
					p.stages[stage][i].ElapsedNS = 100
				} else {
					p.stages[stage][i].ElapsedNS = 150
				}
			}
		}
		p.stages["sent"][0].ElapsedNS = 99
		p.write(t)
		got, c := invoke(t, p.dir)
		if c != 1 || got.Status != "FAIL" || got.TimeOrder != 1 {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("terminal before planned fails", func(t *testing.T) {
		p := makePack(t, 1)
		for _, stage := range stages {
			for i := range p.stages[stage] {
				if stage == "planned" {
					p.stages[stage][i].ElapsedNS = 100
				} else {
					p.stages[stage][i].ElapsedNS = 150
				}
			}
		}
		p.stages["terminal"][0].ElapsedNS = 99
		p.write(t)
		got, c := invoke(t, p.dir)
		if c != 1 || got.Status != "FAIL" || got.TimeOrder != 1 {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("receipt cannot be reused across events", func(t *testing.T) {
		p := makePack(t, 2)
		a, b := p.stages["accepted"], p.stages["terminal"]
		a[1].ReceiptID = a[0].ReceiptID
		b[1].ReceiptID = b[0].ReceiptID
		p.set("accepted", a, t)
		p.set("terminal", b, t)
		got, c := invoke(t, p.dir)
		if c != 1 || got.Status != "FAIL" || got.ReceiptConflicts != 1 {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("terminal failure is semantic fail", func(t *testing.T) {
		p := makePack(t, 1)
		xs := append([]record(nil), p.stages["terminal"]...)
		xs[0].Outcome = "failure"
		p.set("terminal", xs, t)
		got, c := invoke(t, p.dir)
		if c != 1 || got.Status != "FAIL" || got.TerminalFailures != 1 {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("same effect id with contradictory row is invalid", func(t *testing.T) {
		p := makePack(t, 1)
		xs := append([]record(nil), p.stages["sideeffects"]...)
		changed := xs[0]
		changed.ElapsedNS++
		xs = append(xs, changed)
		p.set("sideeffects", xs, t)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
}

func TestReconcileThousandsOfDistinctEffectsForOneEvent(t *testing.T) {
	p := makePack(t, 1)
	xs := append([]record(nil), p.stages["sideeffects"]...)
	for i := 0; i < 3000; i++ {
		v := xs[0]
		v.EffectID = fmt.Sprintf("extra-effect-%05d", i)
		xs = append(xs, v)
	}
	p.set("sideeffects", xs, t)
	got, code := invoke(t, p.dir)
	if code != 1 || got.Status != "FAIL" || got.DuplicateEffects != 3000 {
		t.Fatalf("got=%+v code=%d", got, code)
	}
}

func TestReconcileInvalidAndBlockedInputs(t *testing.T) {
	t.Run("missing file blocked", func(t *testing.T) {
		p := makePack(t, 1)
		_ = os.Remove(filepath.Join(p.dir, "terminal.ndjson"))
		got, c := invoke(t, p.dir)
		if c != 3 || got.Status != "BLOCKED" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("hash mismatch invalid", func(t *testing.T) {
		p := makePack(t, 1)
		f := filepath.Join(p.dir, "sent.ndjson")
		b, _ := os.ReadFile(f)
		b = append(b, []byte(" ")...)
		_ = os.WriteFile(f, b, 0600)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("duplicate key invalid", func(t *testing.T) {
		p := makePack(t, 1)
		_ = os.WriteFile(filepath.Join(p.dir, "manifest.json"), []byte(`{"schema_version":1,"schema_version":1}`), 0600)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("unknown manifest field invalid", func(t *testing.T) {
		p := makePack(t, 1)
		raw, _ := os.ReadFile(filepath.Join(p.dir, "manifest.json"))
		_ = os.WriteFile(filepath.Join(p.dir, "manifest.json"), append(raw[:len(raw)-1], []byte(`,"extra":true}`)...), 0600)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("uppercase alias cannot replace canonical key", func(t *testing.T) {
		p := makePack(t, 1)
		f := filepath.Join(p.dir, "manifest.json")
		raw, _ := os.ReadFile(f)
		if !bytes.Contains(raw, []byte(`"run_id"`)) {
			t.Fatal("fixture manifest missing run_id")
		}
		_ = os.WriteFile(f, bytes.Replace(raw, []byte(`"run_id"`), []byte(`"RUN_ID"`), 1), 0600)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("uppercase alias alongside canonical key invalid", func(t *testing.T) {
		p := makePack(t, 1)
		f := filepath.Join(p.dir, "manifest.json")
		raw, _ := os.ReadFile(f)
		raw = append(raw[:len(raw)-1], []byte(`,"RUN_ID":"shadow"}`)...)
		_ = os.WriteFile(f, raw, 0600)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("symlink invalid", func(t *testing.T) {
		p := makePack(t, 1)
		f := filepath.Join(p.dir, "planned.ndjson")
		b, _ := os.ReadFile(f)
		real := filepath.Join(p.dir, "target")
		_ = os.WriteFile(real, b, 0600)
		_ = os.Remove(f)
		if err := os.Symlink(real, f); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("Windows symlink fixture requires additional permissions: %v", err)
			}
			t.Fatal(err)
		}
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("exact duplicate record invalid", func(t *testing.T) {
		p := makePack(t, 1)
		f := filepath.Join(p.dir, "sent.ndjson")
		b, _ := os.ReadFile(f)
		b = append(b, b...)
		_ = os.WriteFile(f, b, 0600)
		sum := sha256.Sum256(b)
		p.m.SHA256["sent"] = hex.EncodeToString(sum[:])
		mb, _ := json.Marshal(p.m)
		_ = os.WriteFile(filepath.Join(p.dir, "manifest.json"), mb, 0600)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("oversized line invalid", func(t *testing.T) {
		p := makePack(t, 1)
		f := filepath.Join(p.dir, "sent.ndjson")
		b := bytes.Repeat([]byte{'x'}, lineLimit+1)
		_ = os.WriteFile(f, b, 0600)
		sum := sha256.Sum256(b)
		p.m.SHA256["sent"] = hex.EncodeToString(sum[:])
		mb, _ := json.Marshal(p.m)
		_ = os.WriteFile(filepath.Join(p.dir, "manifest.json"), mb, 0600)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("oversized manifest invalid", func(t *testing.T) {
		p := makePack(t, 1)
		_ = os.WriteFile(filepath.Join(p.dir, "manifest.json"), bytes.Repeat([]byte{' '}, manifestLimit+1), 0600)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
	t.Run("missing required field invalid", func(t *testing.T) {
		p := makePack(t, 1)
		_ = os.WriteFile(filepath.Join(p.dir, "planned.ndjson"), []byte(`{"run_id":"run-1","event_id":"e","tenant_id":"t","device_id":"d","stream_id":"s","clock_id":"clock-shared"}`+"\n"), 0600)
		b, _ := os.ReadFile(filepath.Join(p.dir, "planned.ndjson"))
		sum := sha256.Sum256(b)
		p.m.SHA256["planned"] = hex.EncodeToString(sum[:])
		mb, _ := json.Marshal(p.m)
		_ = os.WriteFile(filepath.Join(p.dir, "manifest.json"), mb, 0600)
		got, c := invoke(t, p.dir)
		if c != 2 || got.Status != "INVALID" {
			t.Fatalf("got=%+v code=%d", got, c)
		}
	})
}
