package offlineupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"encore.app/internal/tenantidentity"
	"github.com/google/uuid"
)

// TenantImportInput is a private, verified source snapshot. Raw source rows may
// contain credentials: never log this value or persist it in the native owner.
// Only metadata and provenance are imported; runtime ownership stays Community.
type TenantImportInput struct {
	SourceDatabase      string                  `json:"source_database"`
	TargetDatabase      string                  `json:"target_database"`
	SourceDeploymentID  string                  `json:"source_deployment_id"`
	SourceTenantID      string                  `json:"source_tenant_id"`
	SourceArchiveSHA256 string                  `json:"source_archive_sha256"`
	TargetArchiveSHA256 string                  `json:"target_archive_sha256"`
	ResolutionsSHA256   string                  `json:"resolutions_sha256"`
	RecordedAt          time.Time               `json:"recorded_at"`
	Groups              []json.RawMessage       `json:"groups"`
	Devices             []json.RawMessage       `json:"devices"`
	Templates           []json.RawMessage       `json:"templates"`
	Members             []SourceGroupMember     `json:"members"`
	NameResolutions     []GroupNameResolution   `json:"name_resolutions,omitempty"`
	ParentResolutions   []GroupParentResolution `json:"parent_resolutions,omitempty"`
	NameFromNumber      []string                `json:"name_from_number,omitempty"`
	ExcludedMembers     []MemberExclusion       `json:"excluded_members,omitempty"`
	ExcludedGateways    []DeviceRelation        `json:"excluded_gateways,omitempty"`
}

// ImportedOrigin contains no access parameters, credentials or fabricated
// native model/provider capability. The source owner remains authoritative.
type ImportedOrigin struct {
	TenantID              string
	DeviceID              string
	SourceOwner           string
	SourceDatabase        string
	SourceDeploymentID    string
	SourceTenantID        string
	SourceDeviceID        string
	SourceTemplateID      *string
	SourceTemplateName    *string
	SourceProtocol        *string
	SourceAccessWay       *string
	SourceServiceAccessID *string
	SourceRowSHA256       string
	SourceTemplateSHA256  *string
	SourceArchiveSHA256   string
	ImportedAt            time.Time `gorm:"autoCreateTime:false"`
	ImportedBy            string
}

type TenantImportPlan struct {
	TenantID string
	Actor    string
	Digest   string
	Groups   []PlannedGroup
	Devices  []PlannedDevice
	Origins  []ImportedOrigin
	Members  []GroupMember
	Gateways []GatewayMember
}

func importDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func PlanTenantImport(in TenantImportInput) (TenantImportPlan, error) {
	p := TenantImportPlan{TenantID: tenantidentity.CanonicalIdentity("tenant", in.SourceTenantID), Actor: "migration:" + in.SourceArchiveSHA256}
	if in.SourceDatabase != "ThingsPanel" || in.TargetDatabase == "" || in.TargetDatabase == in.SourceDatabase ||
		in.SourceTenantID == "" || strings.TrimSpace(in.SourceTenantID) != in.SourceTenantID ||
		in.SourceDeploymentID == "" || len(in.SourceDeploymentID) > 128 ||
		in.RecordedAt.IsZero() || in.RecordedAt.Nanosecond()%1000 != 0 {
		return p, fmt.Errorf("explicit source/target, tenant, deployment and fixed record time required")
	}
	for _, h := range []string{in.SourceArchiveSHA256, in.TargetArchiveSHA256, in.ResolutionsSHA256} {
		if !sha256Pattern.MatchString(h) {
			return p, fmt.Errorf("verified private archive digests required")
		}
	}
	if len(in.Groups) == 0 || len(in.Groups) > 10000 || len(in.Devices) > 10000 || len(in.Members) > 100000 || len(in.Templates) > 10000 {
		return p, fmt.Errorf("tenant import closure is empty or exceeds bounds")
	}
	// No quarantines here: this import is the already resolved exact tenant
	// closure. A foreign/missing endpoint rejects the whole transaction.
	groupSet, deviceSet := map[string]bool{}, map[string]bool{}
	for _, collection := range []struct {
		kind string
		rows []json.RawMessage
		ids  map[string]bool
	}{{"group", in.Groups, groupSet}, {"device", in.Devices, deviceSet}, {"template", in.Templates, map[string]bool{}}} {
		for _, raw := range collection.rows {
			var id struct {
				ID     string `json:"id"`
				Tenant string `json:"tenant_id"`
			}
			if json.Unmarshal(raw, &id) != nil || id.Tenant != in.SourceTenantID || id.ID == "" || collection.ids[id.ID] {
				return p, fmt.Errorf("source identity/tenant closure is invalid")
			}
			u, e := uuid.Parse(id.ID)
			if e != nil || u == uuid.Nil || u.String() != id.ID {
				return p, fmt.Errorf("source UUIDs must be preserved exactly")
			}
			collection.ids[id.ID] = true
		}
	}
	groups, e := planGroupRows(GroupInput{SourceArchiveSHA256: in.SourceArchiveSHA256, Groups: in.Groups, NameResolutions: in.NameResolutions, ParentResolutions: in.ParentResolutions})
	if e != nil {
		return p, e
	}
	devices, e := planDeviceIdentityRows(DeviceIdentityInput{SourceArchiveSHA256: in.SourceArchiveSHA256, Devices: in.Devices, Templates: in.Templates, NameFromNumber: in.NameFromNumber, ExcludedGatewayRelations: in.ExcludedGateways})
	if e != nil {
		return p, e
	}
	var gateways []SourceGateway
	for _, raw := range in.Devices {
		var d sourceDevice
		_ = json.Unmarshal(raw, &d)
		if text(d.ParentID) != "" {
			gateways = append(gateways, SourceGateway{TenantID: d.TenantID, DeviceID: d.ID, ParentID: text(d.ParentID), Address: text(d.SubdeviceAddress)})
		}
	}
	excludedMembers := map[string]bool{}
	for _, m := range in.ExcludedMembers {
		excludedMembers[m.GroupID+"/"+m.DeviceID] = true
	}
	for _, m := range in.Members {
		if m.TenantID != in.SourceTenantID || !groupSet[m.GroupID] || (!deviceSet[m.DeviceID] && !excludedMembers[m.GroupID+"/"+m.DeviceID]) {
			return p, fmt.Errorf("membership lies outside tenant closure")
		}
	}
	relations, e := planRelationRows(RelationsInput{SourceArchiveSHA256: in.SourceArchiveSHA256, RecordedAt: in.RecordedAt, Members: in.Members, Gateways: gateways, ExcludedMembers: in.ExcludedMembers, ExcludedGateways: in.ExcludedGateways})
	if e != nil {
		return p, e
	}
	templates := map[string]json.RawMessage{}
	for _, raw := range in.Templates {
		var t sourceTemplateKind
		_ = json.Unmarshal(raw, &t)
		templates[t.ID] = raw
	}
	for _, raw := range in.Devices {
		var d struct {
			sourceDevice
			ServiceAccessID *string `json:"service_access_id"`
			Protocol        *string `json:"protocol"`
			AccessWay       *string `json:"access_way"`
		}
		if json.Unmarshal(raw, &d) != nil {
			return p, fmt.Errorf("invalid source device")
		}
		o := ImportedOrigin{TenantID: p.TenantID, DeviceID: d.ID, SourceOwner: "community", SourceDatabase: in.SourceDatabase, SourceDeploymentID: in.SourceDeploymentID, SourceTenantID: in.SourceTenantID, SourceDeviceID: d.ID, SourceServiceAccessID: nonempty(d.ServiceAccessID), SourceRowSHA256: rawHash(raw), SourceArchiveSHA256: in.SourceArchiveSHA256, ImportedAt: in.RecordedAt.UTC(), ImportedBy: p.Actor}
		o.SourceProtocol = nonempty(d.Protocol)
		o.SourceAccessWay = nonempty(d.AccessWay)
		if id := text(d.ConfigID); id != "" {
			traw, ok := templates[id]
			if !ok {
				return p, fmt.Errorf("source template closure incomplete")
			}
			var t struct {
				Name     *string `json:"name"`
				Protocol *string `json:"protocol_type"`
			}
			if json.Unmarshal(traw, &t) != nil {
				return p, fmt.Errorf("invalid source template")
			}
			h := rawHash(traw)
			o.SourceTemplateID = &id
			o.SourceTemplateName = nonempty(t.Name)
			if o.SourceProtocol == nil {
				o.SourceProtocol = nonempty(t.Protocol)
			}
			o.SourceTemplateSHA256 = &h
		}
		p.Origins = append(p.Origins, o)
	}
	p.Groups, p.Devices, p.Members, p.Gateways = groups.Groups, devices.Devices, relations.Members, relations.Gateways
	p.Digest = importDigest(in)
	return p, nil
}

func rawHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func nonempty(p *string) *string {
	if p == nil || *p == "" {
		return nil
	}
	return p
}
