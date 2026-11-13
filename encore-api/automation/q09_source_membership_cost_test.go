package automation

import (
	"context"
	"encore.app/automation/ports"
	"encore.app/automation/runtime"
	"fmt"
	"testing"
	"time"
)

type q09CountSourceDirectory struct {
	ports.Directory
	exact, wide int
}

func (d *q09CountSourceDirectory) ByIDs(ctx context.Context, tenant string, ids []string) ([]ports.Device, error) {
	d.exact++
	return d.Directory.ByIDs(ctx, tenant, ids)
}
func (d *q09CountSourceDirectory) ByDeviceConfig(ctx context.Context, tenant string, ids []string) ([]ports.Device, error) {
	d.wide++
	return d.Directory.ByDeviceConfig(ctx, tenant, ids)
}
func TestQ09ManyRulesReadOneSourceDevicePerEvent(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 6; i++ {
		def := e.baseDefinition()
		def.Targets.DeviceIDs = nil
		def.Targets.DeviceConfigIDs = []string{"cfg-1"}
		e.createRule(def)
	}
	for i := 0; i < 3000; i++ {
		id := fmt.Sprintf("noise-%d", i)
		e.dir.devices[id] = ports.Device{ID: id, TenantID: e.tenant, DeviceConfigID: "cfg-1"}
	}
	dir := &q09CountSourceDirectory{Directory: e.dir}
	e.rt = runtime.New(e.st, e.cmds, e.notif, dir, func() time.Time { return e.now })
	e.ingest(runtime.NewID(), e.dev1, "temperature", 20.0, "")
	if dir.exact != 1 || dir.wide != 0 {
		t.Fatalf("6 candidates scanned directory exact%d wide%d", dir.exact, dir.wide)
	}
	e.ingest(runtime.NewID(), e.dev1, "temperature", 21.0, "")
	if dir.exact != 2 || dir.wide != 0 || e.cmds.count() != 0 {
		t.Fatal("event cache leaked or false condition caused action")
	}
}
