// Package offlineupgrade owns offline conversion into the native metadata
// schema. It is deliberately independent of the running Encore service.
package offlineupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"encore.app/internal/tenantidentity"
	"github.com/google/uuid"
)

var rehearsalDatabase = regexp.MustCompile(`^tp_upgrade_rehearsal_[0-9]{14}$`)
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// GroupInput is assembled only from verified private restore receipts and the
// approved quarantine archive. Original source rows stay in the private bundle.
type GroupInput struct {
	SourceDatabase      string                `json:"source_database"`
	TargetDatabase      string                `json:"target_database"`
	SourceArchiveSHA256 string                `json:"source_archive_sha256"`
	TargetArchiveSHA256 string                `json:"target_archive_sha256"`
	QuarantineSHA256    string                `json:"quarantine_sha256"`
	Groups              []json.RawMessage     `json:"groups"`
	ExcludedGroups      []string              `json:"excluded_groups"`
	NameResolutions     []GroupNameResolution `json:"name_resolutions,omitempty"`
}

// A resolution must be explicitly reviewed; no automatic suffixing, dropping,
// or coalescing of distinct groups is performed by the converter.
type GroupNameResolution struct {
	SourceID     string `json:"source_id"`
	OriginalName string `json:"original_name"`
	Name         string `json:"name"`
}

type sourceGroup struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	ParentID    *string    `json:"parent_id"`
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

type Group struct {
	ID              string     `gorm:"column:id"`
	TenantID        string     `gorm:"column:tenant_id"`
	ParentID        *string    `gorm:"column:parent_id"`
	Name            string     `gorm:"column:name"`
	Description     *string    `gorm:"column:description"`
	SortOrder       int        `gorm:"column:sort_order"`
	ResourceVersion int64      `gorm:"column:resource_version"`
	CreatedBy       string     `gorm:"column:created_by"`
	UpdatedBy       string     `gorm:"column:updated_by"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
}

type PlannedGroup struct {
	Row             Group
	SourceID        string
	SourceRowSHA256 string
}

type GroupPlan struct {
	Input  GroupInput
	Groups []PlannedGroup // parents before children
	Actor  string
}

// PlanGroups never repairs ownership, names, missing timestamps, or hierarchy.
// The legacy documented root sentinel "0" becomes NULL. The source has no
// sibling order, so native sort_order is zero (normal name/ID tie ordering).
// Actors identify this conversion, not a guessed historical creator.
func PlanGroups(input GroupInput) (GroupPlan, error) {
	p := GroupPlan{Input: input, Actor: "migration:" + input.SourceArchiveSHA256}
	if !rehearsalDatabase.MatchString(input.SourceDatabase) ||
		!rehearsalDatabase.MatchString(input.TargetDatabase) || input.SourceDatabase == input.TargetDatabase {
		return p, fmt.Errorf("distinct isolated source and target databases required")
	}
	for _, hash := range []string{input.SourceArchiveSHA256, input.TargetArchiveSHA256, input.QuarantineSHA256} {
		if !sha256Pattern.MatchString(hash) {
			return p, fmt.Errorf("verified archive digests required")
		}
	}
	excluded := map[string]bool{}
	for _, id := range input.ExcludedGroups {
		if id == "" || excluded[id] {
			return p, fmt.Errorf("invalid duplicate group exclusion")
		}
		excluded[id] = true
	}
	rows := map[string]sourceGroup{}
	planned := map[string]PlannedGroup{}
	resolutions := map[string]GroupNameResolution{}
	for _, resolution := range input.NameResolutions {
		if resolution.SourceID == "" || resolution.Name == resolution.OriginalName || excluded[resolution.SourceID] {
			return p, fmt.Errorf("invalid group name resolution")
		}
		if _, ok := resolutions[resolution.SourceID]; ok {
			return p, fmt.Errorf("duplicate group name resolution")
		}
		resolutions[resolution.SourceID] = resolution
	}
	nativeIDs, names := map[string]bool{}, map[string]bool{}
	for _, raw := range input.Groups {
		var source sourceGroup
		if err := json.Unmarshal(raw, &source); err != nil {
			return p, fmt.Errorf("invalid source group row")
		}
		if source.ID == "" {
			return p, fmt.Errorf("empty source group identity")
		}
		if _, ok := rows[source.ID]; ok {
			return p, fmt.Errorf("duplicate source group identity")
		}
		rows[source.ID] = source
		if excluded[source.ID] {
			if strings.TrimSpace(source.TenantID) != "" {
				return p, fmt.Errorf("excluded group ownership changed")
			}
			continue
		}
		if source.TenantID == "" || source.TenantID != strings.TrimSpace(source.TenantID) {
			return p, fmt.Errorf("group %s: missing or ambiguous tenant", source.ID)
		}
		name := source.Name
		if resolution, ok := resolutions[source.ID]; ok {
			if resolution.OriginalName != source.Name {
				return p, fmt.Errorf("reviewed source group name changed")
			}
			name = resolution.Name
		}
		if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 128 {
			return p, fmt.Errorf("group %s: invalid name", source.ID)
		}
		if source.CreatedAt == nil || source.UpdatedAt == nil {
			return p, fmt.Errorf("group %s: missing source timestamp", source.ID)
		}
		id := tenantidentity.CanonicalIdentity("group", source.ID)
		tenant := tenantidentity.CanonicalIdentity("tenant", source.TenantID)
		if id == uuid.Nil.String() || tenant == uuid.Nil.String() {
			return p, fmt.Errorf("zero group or tenant identity")
		}
		if nativeIDs[id] {
			return p, fmt.Errorf("native group identity collision")
		}
		nativeIDs[id] = true
		var parent *string
		if source.ParentID != nil && *source.ParentID != "" && *source.ParentID != "0" {
			v := tenantidentity.CanonicalIdentity("group", *source.ParentID)
			parent = &v
		}
		parentKey := ""
		if parent != nil {
			parentKey = *parent
		}
		nameKey := tenant + "/" + parentKey + "/" + name
		if names[nameKey] {
			return p, fmt.Errorf("duplicate sibling group name")
		}
		names[nameKey] = true
		hash := sha256.Sum256(raw)
		planned[source.ID] = PlannedGroup{SourceID: source.ID, SourceRowSHA256: hex.EncodeToString(hash[:]), Row: Group{
			ID: id, TenantID: tenant, ParentID: parent, Name: name, Description: source.Description,
			ResourceVersion: 1, CreatedBy: p.Actor, UpdatedBy: p.Actor,
			CreatedAt: source.CreatedAt.UTC(), UpdatedAt: source.UpdatedAt.UTC(),
		}}
	}
	for id := range resolutions {
		if _, ok := planned[id]; !ok {
			return p, fmt.Errorf("reviewed group name target absent")
		}
	}
	for id := range excluded {
		if _, ok := rows[id]; !ok {
			return p, fmt.Errorf("approved exclusion absent from source")
		}
	}
	visited := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] == 2 {
			return nil
		}
		if visited[id] == 1 {
			return fmt.Errorf("group hierarchy contains a cycle")
		}
		visited[id] = 1
		row := rows[id]
		if row.ParentID != nil && *row.ParentID != "" && *row.ParentID != "0" {
			parent, ok := rows[*row.ParentID]
			if !ok || excluded[parent.ID] {
				return fmt.Errorf("group parent is absent or quarantined")
			}
			if parent.TenantID != row.TenantID {
				return fmt.Errorf("cross-tenant group parent")
			}
			if err := visit(parent.ID); err != nil {
				return err
			}
		}
		visited[id] = 2
		p.Groups = append(p.Groups, planned[id])
		return nil
	}
	keys := make([]string, 0, len(planned))
	for id := range planned {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		if err := visit(id); err != nil {
			return p, err
		}
	}
	return p, nil
}
