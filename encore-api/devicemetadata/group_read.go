package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

const maxDeviceGroupDepth = 32

type DeviceGroup struct {
	ID                    string                `json:"id"`
	TenantID              string                `json:"tenantId"`
	ParentID              *string               `json:"parentId"`
	Name                  string                `json:"name"`
	Description           *string               `json:"description"`
	SortOrder             int                   `json:"sortOrder"`
	ResourceVersion       int64                 `json:"resourceVersion"`
	CreatedBy             string                `json:"createdBy"`
	UpdatedBy             string                `json:"updatedBy"`
	CreatedAt             time.Time             `json:"createdAt"`
	UpdatedAt             time.Time             `json:"updatedAt"`
	DirectDeviceCount     int                   `json:"directDeviceCount"`
	DescendantDeviceCount int                   `json:"descendantDeviceCount"`
	SubtreeDeviceCount    int                   `json:"subtreeDeviceCount"`
	Path                  []DeviceGroupPathItem `json:"path"`
	PathComplete          bool                  `json:"pathComplete"`
	ActiveAlarmCount      *int64                `json:"activeAlarmCount"`
	AlarmsObservedAt      *time.Time            `json:"alarmsObservedAt"`
}

type DeviceGroupPathItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type GroupTreeNode struct {
	Group           DeviceGroup     `json:"group"`
	Children        []GroupTreeNode `json:"children"`
	HasMoreChildren bool            `json:"hasMoreChildren"`
}

type RequestMeta struct {
	RequestID string `json:"requestId"`
}
type DeviceGroupResponse struct {
	Data DeviceGroup `json:"data"`
	Meta RequestMeta `json:"meta"`
	ETag string      `json:"-" header:"ETag"`
}
type DeviceGroupPage struct {
	Items    []DeviceGroup `json:"items"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
	Total    int           `json:"total"`
}
type DeviceGroupPageResponse struct {
	Data DeviceGroupPage `json:"data"`
	Meta RequestMeta     `json:"meta"`
}
type GroupTree struct {
	Nodes     []GroupTreeNode `json:"nodes"`
	Truncated bool            `json:"truncated"`
}
type GroupTreeResponse struct {
	Data GroupTree   `json:"data"`
	Meta RequestMeta `json:"meta"`
	ETag string      `json:"-" header:"ETag"`
}

type ListDeviceGroupsReq struct {
	Page     string `query:"page"`
	PageSize string `query:"pageSize"`
	ParentID string `query:"parentId"`
	RootOnly bool   `query:"rootOnly"`
	Search   string `query:"search"`
	Sort     string `query:"sort"`
}

func normalizeDeviceGroupPagination(pageValue, pageSizeValue string) (int, int, error) {
	page, pageSize := 1, 20
	if pageValue != "" {
		parsed, err := strconv.Atoi(pageValue)
		if err != nil {
			return 0, 0, &errs.Error{Code: errs.InvalidArgument, Message: "page must be an integer"}
		}
		page = parsed
	}
	if pageSizeValue != "" {
		parsed, err := strconv.Atoi(pageSizeValue)
		if err != nil {
			return 0, 0, &errs.Error{Code: errs.InvalidArgument, Message: "pageSize must be an integer"}
		}
		pageSize = parsed
	}
	if page < 1 || page > 1_000_000 {
		return 0, 0, &errs.Error{Code: errs.InvalidArgument, Message: "page must be between 1 and 1000000"}
	}
	if pageSize < 1 || pageSize > 100 {
		return 0, 0, &errs.Error{Code: errs.InvalidArgument, Message: "pageSize must be between 1 and 100"}
	}
	return page, pageSize, nil
}

type DeviceGroupTreeReq struct {
	RootID   string `query:"rootId"`
	MaxDepth string `query:"maxDepth"`
}

func normalizeDeviceGroupMaxDepth(raw string) (int, error) {
	if raw == "" {
		return maxDeviceGroupDepth, nil
	}
	depth, err := strconv.Atoi(raw)
	if err != nil || depth < 1 || depth > maxDeviceGroupDepth {
		return 0, &errs.Error{Code: errs.InvalidArgument, Message: "maxDepth must be between 1 and 32"}
	}
	return depth, nil
}

type groupRow struct{ group DeviceGroup }

func groupPrincipalForRead() (string, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return "", err
	}
	data := authn.CurrentData()
	if data == nil {
		return "", &errs.Error{Code: errs.Unauthenticated, Message: "missing authenticated principal"}
	}
	if authz.Can(data, "group", "read") {
		return tenantID, nil
	}
	return "", &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to read device groups"}
}

// listDeviceGroups lists groups for the authenticated tenant.
func (s *Service) listDeviceGroups(ctx context.Context, req *ListDeviceGroupsReq, requestID string) (*DeviceGroupPageResponse, error) {
	tenantID, err := groupPrincipalForRead()
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &ListDeviceGroupsReq{}
	}
	page, pageSize, err := normalizeDeviceGroupPagination(req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	search := strings.TrimSpace(req.Search)
	if len(search) > 128 {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "search must be at most 128 characters"}
	}
	if req.Sort != "" && req.Sort != "sortOrder" && req.Sort != "-sortOrder" && req.Sort != "name" && req.Sort != "-name" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "unsupported group sort"}
	}
	if req.ParentID != "" && req.RootOnly {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "parentId conflicts with rootOnly"}
	}
	parentID, err := normalizeDeviceGroupID(req.ParentID, true)
	if err != nil {
		return nil, err
	}
	order := "sort_order ASC, id ASC"
	switch req.Sort {
	case "-sortOrder":
		order = "sort_order DESC, id ASC"
	case "name":
		order = "name ASC, id ASC"
	case "-name":
		order = "name DESC, id ASC"
	}
	where := ` FROM device_groups g WHERE g.tenant_id=$1 AND g.deleted_at IS NULL
		AND ($2::uuid IS NULL OR g.parent_id=$2::uuid)
		AND (NOT $3 OR g.parent_id IS NULL)
		AND ($4='' OR g.name ILIKE '%' || $4 || '%')`
	tx, err := beginTenantGroupTx(ctx, normalizePrincipalID(tenantID), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "list device groups failed")
	}
	defer tx.Rollback()
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)`+where, tenantID, nullableGroupID(parentID), req.RootOnly, search).Scan(&total); err != nil {
		return nil, wrapDBErr(err, "count device groups failed")
	}
	query := `SELECT g.id, g.tenant_id, g.parent_id, g.name, g.description, g.sort_order, g.resource_version,
		g.created_by, g.updated_by, g.created_at, g.updated_at, 0::int` + where + ` ORDER BY ` + order + ` LIMIT $5 OFFSET $6`
	rows, err := tx.QueryContext(ctx, query, tenantID, nullableGroupID(parentID), req.RootOnly, search, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, wrapDBErr(err, "list device groups failed")
	}
	items := make([]DeviceGroup, 0)
	for rows.Next() {
		var g DeviceGroup
		if err := rows.Scan(&g.ID, &g.TenantID, &g.ParentID, &g.Name, &g.Description, &g.SortOrder, &g.ResourceVersion, &g.CreatedBy, &g.UpdatedBy, &g.CreatedAt, &g.UpdatedAt, &g.DirectDeviceCount); err != nil {
			return nil, wrapDBErr(err, "list device groups failed")
		}
		g.Path, g.PathComplete = []DeviceGroupPathItem{{ID: g.ID, Name: g.Name}}, true
		g.ActiveAlarmCount = nil
		items = append(items, g)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDBErr(err, "list device groups failed")
	}
	if err := rows.Close(); err != nil {
		return nil, wrapDBErr(err, "list device groups failed")
	}
	if items == nil {
		items = []DeviceGroup{}
	}
	if err := hydrateGroupReadMetadata(ctx, tenantID, items, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "list device groups failed")
	}
	return &DeviceGroupPageResponse{Data: DeviceGroupPage{Items: items, Page: page, PageSize: pageSize, Total: total}, Meta: RequestMeta{RequestID: requestID}}, nil
}

// getDeviceGroup reads one group from the authenticated tenant.
func (s *Service) getDeviceGroup(ctx context.Context, groupID, requestID string) (*DeviceGroupResponse, error) {
	groupID, err := normalizeDeviceGroupID(groupID, false)
	if err != nil {
		return nil, err
	}
	tenantID, err := groupPrincipalForRead()
	if err != nil {
		return nil, err
	}
	tx, err := beginTenantGroupTx(ctx, normalizePrincipalID(tenantID), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "read device group failed")
	}
	defer tx.Rollback()
	var g DeviceGroup
	err = tx.QueryRowContext(ctx, `SELECT g.id,g.tenant_id,g.parent_id,g.name,g.description,g.sort_order,g.resource_version,g.created_by,g.updated_by,g.created_at,g.updated_at,0::int
		FROM device_groups g WHERE g.tenant_id=$1 AND g.id=$2 AND g.deleted_at IS NULL`, tenantID, groupID).Scan(&g.ID, &g.TenantID, &g.ParentID, &g.Name, &g.Description, &g.SortOrder, &g.ResourceVersion, &g.CreatedBy, &g.UpdatedBy, &g.CreatedAt, &g.UpdatedAt, &g.DirectDeviceCount)
	if err != nil {
		return nil, wrapDBErr(err, "device group not found")
	}
	one := []DeviceGroup{g}
	if err := hydrateGroupReadMetadata(ctx, tenantID, one, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "read device group failed")
	}
	g = one[0]
	return &DeviceGroupResponse{Data: g, Meta: RequestMeta{RequestID: requestID}, ETag: groupReadETag(g)}, nil
}

// getDeviceGroupTree returns a bounded tree and explicitly identifies truncation.
func (s *Service) getDeviceGroupTree(ctx context.Context, req *DeviceGroupTreeReq, requestID string) (*GroupTreeResponse, error) {
	tenantID, err := groupPrincipalForRead()
	if err != nil {
		return nil, err
	}
	if req == nil {
		req = &DeviceGroupTreeReq{}
	}
	rootID, err := normalizeDeviceGroupID(req.RootID, true)
	if err != nil {
		return nil, err
	}
	depth, err := normalizeDeviceGroupMaxDepth(req.MaxDepth)
	if err != nil {
		return nil, err
	}
	tx, err := beginTenantGroupTx(ctx, normalizePrincipalID(tenantID), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "read device group tree failed")
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT g.id,g.tenant_id,g.parent_id,g.name,g.description,g.sort_order,g.resource_version,g.created_by,g.updated_by,g.created_at,g.updated_at,0::int
		FROM device_groups g WHERE g.tenant_id=$1 AND g.deleted_at IS NULL ORDER BY g.sort_order,g.id`, tenantID)
	if err != nil {
		return nil, wrapDBErr(err, "read device group tree failed")
	}
	groups := make([]DeviceGroup, 0)
	for rows.Next() {
		var g DeviceGroup
		if err := rows.Scan(&g.ID, &g.TenantID, &g.ParentID, &g.Name, &g.Description, &g.SortOrder, &g.ResourceVersion, &g.CreatedBy, &g.UpdatedBy, &g.CreatedAt, &g.UpdatedAt, &g.DirectDeviceCount); err != nil {
			return nil, wrapDBErr(err, "read device group tree failed")
		}
		g.PathComplete = true
		g.ActiveAlarmCount = nil
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDBErr(err, "read device group tree failed")
	}
	if err := rows.Close(); err != nil {
		return nil, wrapDBErr(err, "read device group tree failed")
	}
	if err := hydrateGroupReadMetadata(ctx, tenantID, groups, tx); err != nil {
		return nil, err
	}
	if req.RootID != "" {
		found := false
		for _, g := range groups {
			if g.ID == rootID {
				found = true
				break
			}
		}
		if !found {
			return nil, &errs.Error{Code: errs.NotFound, Message: "device group not found"}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "read device group tree failed")
	}
	nodes, truncated := buildGroupReadTree(groups, rootID, depth)
	tree := GroupTree{Nodes: nodes, Truncated: truncated}
	return &GroupTreeResponse{Data: tree, Meta: RequestMeta{RequestID: requestID}, ETag: groupTreeETag(tree)}, nil
}

func nullableGroupID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func normalizeDeviceGroupID(raw string, optional bool) (string, error) {
	if raw == "" && optional {
		return "", nil
	}
	parsed, err := uuid.Parse(raw)
	if err != nil || strings.TrimSpace(raw) != raw {
		return "", &errs.Error{Code: errs.InvalidArgument, Message: "group ID must be a UUID"}
	}
	return parsed.String(), nil
}
func groupReadETag(group DeviceGroup) string { return groupJSONETag(group) }

func groupTreeETag(tree GroupTree) string { return groupJSONETag(tree) }

func groupJSONETag(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return `"sha256-` + hex.EncodeToString(digest[:]) + `"`
}

type groupReadQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func hydrateGroupReadMetadata(ctx context.Context, tenantID string, groups []DeviceGroup, queryer groupReadQueryer) error {
	var allowedDeviceIDs []string
	if data := authn.CurrentData(); data != nil {
		for _, raw := range data.AuthorizedDeviceIDs {
			id, err := nativeReadUUID(raw)
			if err != nil {
				return &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated device scope"}
			}
			allowedDeviceIDs = append(allowedDeviceIDs, id)
		}
	}
	return hydrateGroupReadMetadataScoped(ctx, tenantID, groups, queryer, allowedDeviceIDs)
}

func hydrateGroupReadMetadataScoped(ctx context.Context, tenantID string, groups []DeviceGroup, queryer groupReadQueryer, allowedDeviceIDs []string) error {
	allRows, err := queryer.QueryContext(ctx, `SELECT id,parent_id,name FROM device_groups WHERE tenant_id=$1 AND deleted_at IS NULL`, tenantID)
	if err != nil {
		return wrapDBErr(err, "load device group ancestry failed")
	}
	defer allRows.Close()
	type node struct {
		parent *string
		name   string
	}
	all := map[string]node{}
	for allRows.Next() {
		var id, name string
		var parent *string
		if err := allRows.Scan(&id, &parent, &name); err != nil {
			return wrapDBErr(err, "load device group ancestry failed")
		}
		all[id] = node{parent: parent, name: name}
	}
	if err := allRows.Err(); err != nil {
		return wrapDBErr(err, "load device group ancestry failed")
	}
	memberSQL := `SELECT m.group_id,m.device_id FROM device_group_members m
		JOIN devices d ON d.tenant_id=m.tenant_id AND d.id=m.device_id
		WHERE m.tenant_id=$1 AND d.deleted_at IS NULL`
	memberArgs := []any{tenantID}
	if len(allowedDeviceIDs) > 0 {
		memberSQL += ` AND d.id=ANY(string_to_array($2,',')::uuid[])`
		memberArgs = append(memberArgs, strings.Join(allowedDeviceIDs, ","))
	}
	memberRows, err := queryer.QueryContext(ctx, memberSQL, memberArgs...)
	if err != nil {
		return wrapDBErr(err, "load device group member counts failed")
	}
	defer memberRows.Close()
	direct := map[string]map[string]struct{}{}
	for memberRows.Next() {
		var groupID, deviceID string
		if err := memberRows.Scan(&groupID, &deviceID); err != nil {
			return wrapDBErr(err, "load device group member counts failed")
		}
		if direct[groupID] == nil {
			direct[groupID] = map[string]struct{}{}
		}
		direct[groupID][deviceID] = struct{}{}
	}
	if err := memberRows.Err(); err != nil {
		return wrapDBErr(err, "load device group member counts failed")
	}
	descendants := map[string]map[string]struct{}{}
	for childID, devices := range direct {
		visited := map[string]bool{childID: true}
		for parent := all[childID].parent; parent != nil; parent = all[*parent].parent {
			if visited[*parent] {
				return &errs.Error{Code: errs.FailedPrecondition, Message: "device group tree contains a cycle"}
			}
			visited[*parent] = true
			if descendants[*parent] == nil {
				descendants[*parent] = map[string]struct{}{}
			}
			for deviceID := range devices {
				descendants[*parent][deviceID] = struct{}{}
			}
		}
	}
	for i := range groups {
		g := &groups[i]
		seen := map[string]bool{}
		path := make([]DeviceGroupPathItem, 0, maxDeviceGroupDepth)
		current := g.ID
		for current != "" {
			if seen[current] {
				return &errs.Error{Code: errs.FailedPrecondition, Message: "device group tree contains a cycle"}
			}
			seen[current] = true
			item, ok := all[current]
			if !ok {
				return &errs.Error{Code: errs.FailedPrecondition, Message: "device group parent is missing"}
			}
			path = append(path, DeviceGroupPathItem{ID: current, Name: item.name})
			if len(path) > maxDeviceGroupDepth {
				return &errs.Error{Code: errs.FailedPrecondition, Message: "device group depth exceeds 32"}
			}
			if item.parent == nil {
				break
			}
			current = *item.parent
		}
		for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
			path[left], path[right] = path[right], path[left]
		}
		g.Path = path
		g.PathComplete = true
		g.DirectDeviceCount = len(direct[g.ID])
		g.DescendantDeviceCount = len(descendants[g.ID])
		g.SubtreeDeviceCount = len(descendants[g.ID])
		for deviceID := range direct[g.ID] {
			if _, inDescendants := descendants[g.ID][deviceID]; !inDescendants {
				g.SubtreeDeviceCount++
			}
		}
		g.ActiveAlarmCount = nil
		g.AlarmsObservedAt = nil
	}
	return nil
}

func buildGroupReadTree(groups []DeviceGroup, rootID string, maxDepth int) ([]GroupTreeNode, bool) {
	children := map[string][]DeviceGroup{}
	for _, g := range groups {
		parent := ""
		if g.ParentID != nil {
			parent = *g.ParentID
		}
		children[parent] = append(children[parent], g)
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool {
			a, b := children[parent][i], children[parent][j]
			if a.SortOrder != b.SortOrder {
				return a.SortOrder < b.SortOrder
			}
			return a.ID < b.ID
		})
	}
	var build func(string, int) ([]GroupTreeNode, bool)
	var buildNode func(DeviceGroup, int) (GroupTreeNode, bool)
	buildNode = func(g DeviceGroup, depth int) (GroupTreeNode, bool) {
		node := GroupTreeNode{Group: g, Children: []GroupTreeNode{}}
		if depth >= maxDepth {
			node.HasMoreChildren = len(children[g.ID]) > 0
			return node, node.HasMoreChildren
		}
		nested, truncated := build(g.ID, depth+1)
		node.Children = nested
		node.HasMoreChildren = truncated
		return node, truncated
	}
	build = func(parent string, depth int) ([]GroupTreeNode, bool) {
		nodes := make([]GroupTreeNode, 0, len(children[parent]))
		truncated := false
		for _, g := range children[parent] {
			node, cut := buildNode(g, depth)
			truncated = truncated || cut
			nodes = append(nodes, node)
		}
		return nodes, truncated
	}
	if rootID != "" {
		for _, g := range groups {
			if g.ID == rootID {
				node, truncated := buildNode(g, 1)
				return []GroupTreeNode{node}, truncated
			}
		}
		return []GroupTreeNode{}, false
	}
	return build("", 1)
}
