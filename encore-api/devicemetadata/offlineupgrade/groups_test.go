package offlineupgrade

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"encore.app/internal/tenantidentity"
)

func fixtureInput() GroupInput {
	return GroupInput{SourceDatabase: "tp_upgrade_rehearsal_20000101000000", TargetDatabase: "tp_upgrade_rehearsal_20000101000001",
		SourceArchiveSHA256: strings.Repeat("a", 64), TargetArchiveSHA256: strings.Repeat("b", 64), QuarantineSHA256: strings.Repeat("c", 64)}
}

func fixtureGroup(id, tenant, parent, name string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"id": id, "tenant_id": tenant, "parent_id": parent, "name": name, "description": nil,
		"created_at": "2020-01-01T01:02:03+08:00", "updated_at": "2020-01-02T01:02:03+08:00", "remark": "preserved in private original"})
	return raw
}

func TestPlanGroupsRetainsOwnershipHierarchyAndTimes(t *testing.T) {
	in := fixtureInput()
	in.Groups = []json.RawMessage{fixtureGroup("child", "KlkRL8qX", "root", "child"), fixtureGroup("root", "KlkRL8qX", "0", "root"), fixtureGroup("other", "uOr8IGlG", "0", "root")}
	p, err := PlanGroups(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Groups) != 3 || p.Groups[0].SourceID != "root" || p.Groups[1].SourceID != "child" {
		t.Fatal("parents must precede children")
	}
	if p.Groups[0].Row.ParentID != nil || *p.Groups[1].Row.ParentID != p.Groups[0].Row.ID {
		t.Fatal("root sentinel conversion incorrect")
	}
	if p.Groups[0].Row.TenantID == p.Groups[2].Row.TenantID {
		t.Fatal("legacy tenant collision reintroduced")
	}
	if p.Groups[0].Row.TenantID != tenantidentity.CanonicalIdentity("tenant", "KlkRL8qX") {
		t.Fatal("owner differs from native identity")
	}
	want, _ := time.Parse(time.RFC3339, "2020-01-01T01:02:03+08:00")
	if !p.Groups[0].Row.CreatedAt.Equal(want) || p.Groups[0].Row.CreatedBy != p.Actor {
		t.Fatal("timestamp or migration provenance lost")
	}
}

func TestPlanGroupsRejectsInvalidSourceWithoutRepair(t *testing.T) {
	cases := map[string][]json.RawMessage{
		"cross tenant":       {fixtureGroup("a", "one", "b", "a"), fixtureGroup("b", "two", "0", "b")},
		"missing parent":     {fixtureGroup("a", "one", "missing", "a")},
		"cycle":              {fixtureGroup("a", "one", "b", "a"), fixtureGroup("b", "one", "a", "b")},
		"self cycle":         {fixtureGroup("a", "one", "a", "a")},
		"duplicate identity": {fixtureGroup("a", "one", "0", "a"), fixtureGroup("a", "one", "0", "a")},
		"duplicate sibling":  {fixtureGroup("a", "one", "0", "a"), fixtureGroup("b", "one", "0", "a")},
		"missing owner":      {fixtureGroup("a", "", "0", "a")},
		"empty name":         {fixtureGroup("a", "one", "0", " ")},
		"missing timestamps": {json.RawMessage(`{"id":"a","tenant_id":"one","name":"a"}`)},
	}
	for name, rows := range cases {
		t.Run(name, func(t *testing.T) {
			in := fixtureInput()
			in.Groups = rows
			if _, err := PlanGroups(in); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}

func TestPlanGroupsQuarantineDoesNotExpand(t *testing.T) {
	in := fixtureInput()
	in.Groups = []json.RawMessage{fixtureGroup("excluded", "", "0", "orphan"), fixtureGroup("keep", "tenant", "0", "keep")}
	in.ExcludedGroups = []string{"excluded"}
	p, err := PlanGroups(in)
	if err != nil || len(p.Groups) != 1 || p.Groups[0].SourceID != "keep" {
		t.Fatalf("approved exclusion failed: %v", err)
	}
	in.Groups = append(in.Groups, fixtureGroup("child", "tenant", "excluded", "child"))
	if _, err := PlanGroups(in); err == nil {
		t.Fatal("quarantine silently removed referenced parent")
	}
	in = fixtureInput()
	in.ExcludedGroups = []string{"absent"}
	if _, err := PlanGroups(in); err == nil {
		t.Fatal("missing approved row accepted")
	}
}

func TestPlanGroupsRejectsLiveOrSameDatabase(t *testing.T) {
	for _, name := range []string{"native_main_20261028", "ThingsPanel", "tp_upgrade_rehearsal_20000101000000"} {
		in := fixtureInput()
		in.TargetDatabase = name
		if _, err := PlanGroups(in); err == nil {
			t.Fatalf("unsafe target accepted: %s", name)
		}
	}
}

func TestPlanGroupsOnlyAppliesExplicitMatchingNameResolution(t *testing.T) {
	in := fixtureInput()
	in.Groups = []json.RawMessage{fixtureGroup("a", "tenant", "0", "same"), fixtureGroup("b", "tenant", "0", "same")}
	if _, err := PlanGroups(in); err == nil {
		t.Fatal("unreviewed collision accepted")
	}
	in.NameResolutions = []GroupNameResolution{{SourceID: "b", OriginalName: "same", Name: "same (b)"}}
	p, err := PlanGroups(in)
	if err != nil {
		t.Fatal(err)
	}
	if p.Groups[0].Row.Name != "same" || p.Groups[1].Row.Name != "same (b)" {
		t.Fatal("resolution changed wrong group")
	}
	in.NameResolutions[0].OriginalName = "stale"
	if _, err := PlanGroups(in); err == nil {
		t.Fatal("stale resolution accepted")
	}
}
