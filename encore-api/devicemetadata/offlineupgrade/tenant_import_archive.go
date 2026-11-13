package offlineupgrade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

type TenantImportResolutions struct {
	NameResolutions   []GroupNameResolution   `json:"name_resolutions"`
	ParentResolutions []GroupParentResolution `json:"parent_resolutions"`
	NameFromNumber    []string                `json:"name_from_number"`
	ExcludedMembers   []MemberExclusion       `json:"excluded_members"`
}

// ValidateTenantImportArchives binds every proposed row and resolution to the
// actual private archive bytes. A valid digest alone never authorizes arbitrary
// input rows. Source is read-only; target rows are additionally fenced by the
// transactional strict INSERTs, not overwritten from this historical backup.
func ValidateTenantImportArchives(in TenantImportInput, source, target, resolutions []byte) error {
	if rawHash(source) != in.SourceArchiveSHA256 || rawHash(target) != in.TargetArchiveSHA256 || rawHash(resolutions) != in.ResolutionsSHA256 {
		return fmt.Errorf("private archive digest mismatch")
	}
	var src struct {
		Groups    []json.RawMessage   `json:"groups"`
		Devices   []json.RawMessage   `json:"devices"`
		Templates []json.RawMessage   `json:"templates"`
		Members   []SourceGroupMember `json:"members"`
	}
	if json.Unmarshal(source, &src) != nil || src.Groups == nil || src.Devices == nil || src.Templates == nil || src.Members == nil {
		return fmt.Errorf("source archive collections are missing")
	}
	var dst map[string]json.RawMessage
	if json.Unmarshal(target, &dst) != nil {
		return fmt.Errorf("invalid native target archive")
	}
	for _, key := range []string{"schema", "groups", "devices", "bindings", "members"} {
		if len(dst[key]) == 0 {
			return fmt.Errorf("native target archive collection missing")
		}
	}
	var approved TenantImportResolutions
	d := json.NewDecoder(bytes.NewReader(resolutions))
	d.DisallowUnknownFields()
	if d.Decode(&approved) != nil {
		return fmt.Errorf("invalid reviewed resolution file")
	}
	groups, e := sourceRowsByID(src.Groups)
	if e != nil {
		return e
	}
	devices, e := sourceRowsByID(src.Devices)
	if e != nil {
		return e
	}
	templates, e := sourceRowsByID(src.Templates)
	if e != nil {
		return e
	}
	groupIDs := map[string]bool{}
	for id, raw := range groups {
		if rawTenant(raw) == in.SourceTenantID {
			groupIDs[id] = true
		}
	}
	if !sameSourceRows(in.Groups, groups, groupIDs) {
		return fmt.Errorf("input must contain every original group for this tenant")
	}
	var members []SourceGroupMember
	for _, m := range src.Members {
		if groupIDs[m.GroupID] {
			if m.TenantID != in.SourceTenantID {
				return fmt.Errorf("archive membership tenant differs from group")
			}
			members = append(members, m)
		}
	}
	if !sameUnordered(in.Members, members) {
		return fmt.Errorf("input membership set differs from source archive")
	}
	var exclusions []MemberExclusion
	for _, x := range approved.ExcludedMembers {
		if groupIDs[x.GroupID] {
			exclusions = append(exclusions, x)
		}
	}
	if !sameUnordered(in.ExcludedMembers, exclusions) {
		return fmt.Errorf("membership quarantine differs from reviewed file")
	}
	excluded := map[string]bool{}
	for _, x := range exclusions {
		key := x.GroupID + "/" + x.DeviceID
		if excluded[key] {
			return fmt.Errorf("duplicate approved exclusion")
		}
		excluded[key] = true
		found := false
		for _, m := range members {
			if m.GroupID == x.GroupID && m.DeviceID == x.DeviceID {
				found = true
			}
		}
		// Only an original foreign-owner relation may be quarantined here.
		d, ok := devices[x.DeviceID]
		if !found || !ok || rawTenant(d) == in.SourceTenantID {
			return fmt.Errorf("reviewed exclusion is not an original cross-tenant relationship")
		}
	}
	deviceIDs := map[string]bool{}
	queue := []string{}
	for _, m := range members {
		if !excluded[m.GroupID+"/"+m.DeviceID] {
			queue = append(queue, m.DeviceID)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if deviceIDs[id] {
			continue
		}
		raw, ok := devices[id]
		if !ok || rawTenant(raw) != in.SourceTenantID {
			return fmt.Errorf("device dependency has missing or foreign owner")
		}
		deviceIDs[id] = true
		var row sourceDevice
		if json.Unmarshal(raw, &row) != nil {
			return fmt.Errorf("invalid archived device")
		}
		if text(row.ParentID) != "" {
			queue = append(queue, text(row.ParentID))
		}
	}
	if !sameSourceRows(in.Devices, devices, deviceIDs) {
		return fmt.Errorf("input devices differ from exact membership/ancestor closure")
	}
	templateIDs := map[string]bool{}
	for id := range deviceIDs {
		var row sourceDevice
		_ = json.Unmarshal(devices[id], &row)
		if tid := text(row.ConfigID); tid != "" {
			raw, ok := templates[tid]
			if !ok || rawTenant(raw) != in.SourceTenantID {
				return fmt.Errorf("template dependency has missing or foreign owner")
			}
			templateIDs[tid] = true
		}
	}
	if !sameSourceRows(in.Templates, templates, templateIDs) {
		return fmt.Errorf("input templates differ from exact source dependency closure")
	}
	var names []GroupNameResolution
	for _, x := range approved.NameResolutions {
		if groupIDs[x.SourceID] {
			names = append(names, x)
		}
	}
	var parents []GroupParentResolution
	for _, x := range approved.ParentResolutions {
		if groupIDs[x.SourceID] {
			parents = append(parents, x)
		}
	}
	var numbers []string
	for _, id := range approved.NameFromNumber {
		if deviceIDs[id] {
			numbers = append(numbers, id)
		}
	}
	if !sameUnordered(in.NameResolutions, names) || !sameUnordered(in.ParentResolutions, parents) || !sameUnordered(in.NameFromNumber, numbers) {
		return fmt.Errorf("input transformations differ from reviewed source resolutions")
	}
	_, e = PlanTenantImport(in)
	return e
}

func sourceRowsByID(rows []json.RawMessage) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for _, raw := range rows {
		var row struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &row) != nil || row.ID == "" || out[row.ID] != nil {
			return nil, fmt.Errorf("invalid duplicate source archive identity")
		}
		out[row.ID] = raw
	}
	return out, nil
}
func rawTenant(raw json.RawMessage) string {
	var row struct {
		Tenant string `json:"tenant_id"`
	}
	_ = json.Unmarshal(raw, &row)
	return row.Tenant
}
func semanticJSON(raw []byte) any {
	var out any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&out) != nil {
		return nil
	}
	return out
}
func sameSourceRows(rows []json.RawMessage, source map[string]json.RawMessage, ids map[string]bool) bool {
	if len(rows) != len(ids) {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range rows {
		var r struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &r) != nil || !ids[r.ID] || seen[r.ID] || !reflect.DeepEqual(semanticJSON(raw), semanticJSON(source[r.ID])) {
			return false
		}
		seen[r.ID] = true
	}
	return true
}
func sameUnordered[T any](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, v := range a {
		raw, _ := json.Marshal(v)
		counts[string(raw)]++
	}
	for _, v := range b {
		raw, _ := json.Marshal(v)
		counts[string(raw)]--
		if counts[string(raw)] < 0 {
			return false
		}
	}
	return true
}
