package offlineupgrade

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"encore.app/internal/tenantidentity"
)

type SourceGroupMember struct {
	TenantID string `json:"tenant_id"`
	GroupID  string `json:"group_id"`
	DeviceID string `json:"device_id"`
}

type SourceGateway struct {
	TenantID string `json:"tenant_id"`
	DeviceID string `json:"device_id"`
	ParentID string `json:"parent_id"`
	Address  string `json:"sub_device_addr"`
}

type MemberExclusion struct {
	GroupID  string `json:"group_id"`
	DeviceID string `json:"device_id"`
}

type RelationsInput struct {
	SourceDatabase      string `json:"source_database"`
	TargetDatabase      string `json:"target_database"`
	SourceArchiveSHA256 string `json:"source_archive_sha256"`
	TargetArchiveSHA256 string `json:"target_archive_sha256"`
	QuarantineSHA256    string `json:"quarantine_sha256"`
	// Source relations have no creation timestamp. This is the new metadata
	// record time, explicitly not a fabricated historical relationship time.
	RecordedAt       time.Time           `json:"recorded_at"`
	Members          []SourceGroupMember `json:"members"`
	Gateways         []SourceGateway     `json:"gateways"`
	ExcludedMembers  []MemberExclusion   `json:"excluded_members"`
	ExcludedGateways []DeviceRelation    `json:"excluded_gateways"`
}

type GroupMember struct {
	TenantID  string
	GroupID   string
	DeviceID  string
	CreatedAt time.Time `gorm:"autoCreateTime:false"`
	CreatedBy string
}

type GatewayMember struct {
	TenantID         string
	ChildDeviceID    string
	GatewayDeviceID  string
	SubdeviceAddress string
	CreatedAt        time.Time `gorm:"autoCreateTime:false"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime:false"`
	UpdatedBy        string
}

type RelationsPlan struct {
	Input    RelationsInput
	Actor    string
	Members  []GroupMember
	Gateways []GatewayMember
}

func PlanRelations(in RelationsInput) (RelationsPlan, error) {
	boundary, err := PlanGroups(GroupInput{SourceDatabase: in.SourceDatabase, TargetDatabase: in.TargetDatabase,
		SourceArchiveSHA256: in.SourceArchiveSHA256, TargetArchiveSHA256: in.TargetArchiveSHA256, QuarantineSHA256: in.QuarantineSHA256})
	p := RelationsPlan{Input: in, Actor: boundary.Actor}
	if err != nil {
		return p, err
	}
	if in.RecordedAt.IsZero() || in.RecordedAt.Nanosecond()%1000 != 0 {
		return p, fmt.Errorf("fixed microsecond-precision conversion record time required")
	}
	excluded := map[string]bool{}
	for _, r := range in.ExcludedMembers {
		key := r.GroupID + "/" + r.DeviceID
		if r.GroupID == "" || r.DeviceID == "" || excluded[key] {
			return p, fmt.Errorf("invalid member exclusion")
		}
		excluded[key] = true
	}
	seen := map[string]bool{}
	for _, r := range in.Members {
		key := r.GroupID + "/" + r.DeviceID
		if r.GroupID == "" || r.DeviceID == "" || seen[key] {
			return p, fmt.Errorf("invalid duplicate source membership")
		}
		seen[key] = true
		if excluded[key] {
			continue
		}
		if r.TenantID == "" || strings.TrimSpace(r.TenantID) != r.TenantID {
			return p, fmt.Errorf("membership has ambiguous tenant")
		}
		p.Members = append(p.Members, GroupMember{TenantID: tenantidentity.CanonicalIdentity("tenant", r.TenantID), GroupID: tenantidentity.CanonicalIdentity("group", r.GroupID), DeviceID: tenantidentity.CanonicalIdentity("device", r.DeviceID), CreatedAt: in.RecordedAt.UTC(), CreatedBy: p.Actor})
	}
	for key := range excluded {
		if !seen[key] {
			return p, fmt.Errorf("approved membership absent")
		}
	}
	gatewayExclusions, err := uniqueRelations(in.ExcludedGateways)
	if err != nil {
		return p, err
	}
	seenGateways := map[string]string{}
	addresses := map[string]bool{}
	for _, r := range in.Gateways {
		if r.DeviceID == "" || r.ParentID == "" || seenGateways[r.DeviceID] != "" {
			return p, fmt.Errorf("invalid duplicate source gateway relationship")
		}
		seenGateways[r.DeviceID] = r.ParentID
		if parent, ok := gatewayExclusions[r.DeviceID]; ok {
			if parent != r.ParentID {
				return p, fmt.Errorf("approved gateway changed")
			}
			continue
		}
		if r.TenantID == "" || strings.TrimSpace(r.TenantID) != r.TenantID || r.DeviceID == r.ParentID {
			return p, fmt.Errorf("gateway identity or ownership unresolved")
		}
		if !validDeviceNumber(r.Address) {
			return p, fmt.Errorf("gateway address requires explicit conversion")
		}
		key := r.TenantID + "/" + r.ParentID + "/" + r.Address
		if addresses[key] {
			return p, fmt.Errorf("duplicate gateway child address")
		}
		addresses[key] = true
		p.Gateways = append(p.Gateways, GatewayMember{TenantID: tenantidentity.CanonicalIdentity("tenant", r.TenantID), ChildDeviceID: tenantidentity.CanonicalIdentity("device", r.DeviceID), GatewayDeviceID: tenantidentity.CanonicalIdentity("device", r.ParentID), SubdeviceAddress: r.Address, CreatedAt: in.RecordedAt.UTC(), UpdatedAt: in.RecordedAt.UTC(), UpdatedBy: p.Actor})
	}
	for id, parent := range gatewayExclusions {
		if seenGateways[id] != parent {
			return p, fmt.Errorf("approved gateway absent")
		}
	}
	sort.Slice(p.Members, func(i, j int) bool {
		a, b := p.Members[i], p.Members[j]
		return a.TenantID+"/"+a.GroupID+"/"+a.DeviceID < b.TenantID+"/"+b.GroupID+"/"+b.DeviceID
	})
	sort.Slice(p.Gateways, func(i, j int) bool {
		a, b := p.Gateways[i], p.Gateways[j]
		return a.TenantID+"/"+a.ChildDeviceID < b.TenantID+"/"+b.ChildDeviceID
	})
	return p, nil
}
