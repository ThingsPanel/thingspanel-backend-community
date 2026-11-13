package automation

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"encore.dev/beta/errs"
)

// A real PostgreSQL error at the Directory boundary, with the rule and binding
// store also backed by Encore's real test database. No production state changes.
type previewFailingDirectory struct {
	ports.Directory
	db    *sql.DB
	calls int
}

func (d *previewFailingDirectory) ByDeviceConfig(ctx context.Context, tenant string, ids []string) ([]ports.Device, error) {
	d.calls++
	var result int
	err := d.db.QueryRowContext(ctx, `SELECT 1 / 0`).Scan(&result)
	return nil, err
}

func TestQ09PreviewDirectoryFailureNeverIssuesOrAcceptsToken(t *testing.T) {
	for _, throughBinding := range []bool{false, true} {
		t.Run(fmt.Sprintf("binding_%t", throughBinding), func(t *testing.T) {
			e := newEnv(t)
			cfg := "cfg-preview-" + runtime.NewID()
			dev := e.templateDevice("dev-preview-" + runtime.NewID())
			d := e.dir.devices[dev]
			d.DeviceConfigID = cfg
			e.dir.devices[dev] = d
			body := thresholdRule(dev, "30", 0)
			if !throughBinding {
				body.Targets = domain.TargetSpec{DeviceConfigIDs: []string{cfg}}
			}
			rule := e.createNamedRuleIn(serviceTestTenant, "Preview directory failure", body)
			if throughBinding {
				e.bind(rule, "device_config", cfg, nil)
			}
			before, err := e.svc().PreviewRuleChange(e.ctx(), rule.ID, &ChangePreviewRequest{Definition: body})
			if err != nil || before == nil || before.Token == "" {
				t.Fatalf("healthy preview: %+v %v", before, err)
			}
			failing := &previewFailingDirectory{Directory: e.dir, db: e.st.DB}
			e.rt = runtime.New(e.st, e.cmds, e.notif, failing, func() time.Time { return e.now })
			preview, err := e.svc().PreviewRuleChange(e.ctx(), rule.ID, &ChangePreviewRequest{Definition: body})
			if err == nil || errs.Code(err) != errs.Internal || preview != nil {
				t.Fatalf("directory failure produced usable preview: %+v err=%v", preview, err)
			}
			if failing.calls != 1 {
				t.Fatalf("directory fault not exercised: %d", failing.calls)
			}
			_, err = e.svc().UpdateRule(e.ctx(), rule.ID, &UpdateRuleRequest{Definition: body, Version: rule.Version, PreviewToken: before.Token})
			if err == nil {
				t.Fatal("old preview token saved while scope directory failed")
			}
			stored, err := e.st.GetRule(e.ctx(), serviceTestTenant, rule.ID)
			if err != nil || stored.Version != rule.Version {
				t.Fatalf("failed preview mutated rule: %+v %v", stored, err)
			}
		})
	}
}

func TestQ09PreviewHealthyDirectoryPreservesCountsAndFiftyLimit(t *testing.T) {
	e := newEnv(t)
	cfg := "cfg-preview-many-" + runtime.NewID()
	ids := make([]string, 53)
	for i := range ids {
		ids[i] = e.templateDevice(fmt.Sprintf("dev-preview-%02d-%s", i, runtime.NewID()))
		d := e.dir.devices[ids[i]]
		d.DeviceConfigID = cfg
		e.dir.devices[ids[i]] = d
	}
	body := thresholdRule(ids[0], "30", 0)
	body.Targets = domain.TargetSpec{DeviceIDs: []string{ids[0]}, DeviceConfigIDs: []string{cfg}}
	rule := e.createNamedRuleIn(serviceTestTenant, "Preview count", body)
	e.bind(rule, "device_config", cfg, nil)
	preview, err := e.svc().PreviewRuleChange(e.ctx(), rule.ID, &ChangePreviewRequest{Definition: body})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Scope.DeviceCount != 53 || preview.Scope.TemplateDeviceCount != 53 || len(preview.Scope.DeviceIDs) != 50 || !preview.Scope.Truncated {
		t.Fatalf("count/deduplication/display limit changed: %+v", preview.Scope)
	}
	if !sort.StringsAreSorted(preview.Scope.DeviceIDs) || !preview.RequiresConfirmation || preview.Token == "" {
		t.Fatalf("healthy preview invalid: %+v", preview)
	}
}
