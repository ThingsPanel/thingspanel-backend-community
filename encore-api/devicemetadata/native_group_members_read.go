package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"unicode/utf8"

	"encore.dev"
	"encore.dev/beta/errs"
)

type NativeGroupMember struct {
	Device           NativeDevice `json:"device"`
	DirectMembership bool         `json:"directMembership"`
	MatchedGroupIDs  []string     `json:"matchedGroupIds"`
}
type NativeGroupMemberPage struct {
	Items    []NativeGroupMember `json:"items"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"pageSize"`
	Total    int64               `json:"total"`
}
type NativeGroupMemberPageResponse struct {
	Data NativeGroupMemberPage `json:"data"`
	Meta RequestMeta           `json:"meta"`
}

// This adapter appends membership columns to the shared device scanner, so the
// page and nested Device cannot acquire separate DTO conversion rules.
type nativeGroupMemberScanner struct {
	row     *sql.Rows
	direct  bool
	matched []byte
}

func (s *nativeGroupMemberScanner) Scan(fields ...any) error {
	return s.row.Scan(append(fields, &s.direct, &s.matched)...)
}

func decodeNativeGroupMemberQuery(values url.Values) (nativeDeviceListQuery, error) {
	for key := range values {
		if key != "page" && key != "pageSize" && key != "search" && key != "includeDescendants" {
			return nativeDeviceListQuery{}, invalidGroupQuery("unsupported member query parameter")
		}
	}
	if utf8.RuneCountInString(values.Get("search")) > 128 {
		return nativeDeviceListQuery{}, invalidGroupQuery("search must contain at most 128 characters")
	}
	return decodeNativeDeviceListQuery(values)
}

func readNativeGroupMemberPage(ctx context.Context, identity nativeDeviceReadIdentity, groupID string, query nativeDeviceListQuery, requestID string) (*NativeGroupMemberPageResponse, error) {
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "list group members failed")
	}
	defer tx.Rollback()
	if err := requireNativeGroupRead(ctx, tx, identity.tenantID, groupID); err != nil {
		return nil, err
	}
	if err := requireNativeDeviceReadiness(ctx, tx, identity); err != nil {
		return nil, err
	}
	f := nativeDeviceBaseFilter(identity)
	origin := f.argument(groupID)
	f.cte = nativeGroupSelectionCTE(origin, query.includeDescendants)
	f.where = append(f.where, "EXISTS(SELECT 1 FROM device_group_members m JOIN selected_groups g ON g.id=m.group_id WHERE m.tenant_id=d.tenant_id AND m.device_id=d.id)")
	if search := query.values.Get("search"); search != "" {
		p := f.argument(search)
		f.where = append(f.where, "(strpos(d.name,"+p+")>0 OR strpos(d.device_number,"+p+")>0)")
	}
	page := NativeGroupMemberPage{Items: []NativeGroupMember{}, Page: query.page, PageSize: query.pageSize}
	if err := tx.QueryRowContext(ctx, f.cte+"SELECT count(*) "+nativeDeviceFromSQL+f.predicate(), f.args...).Scan(&page.Total); err != nil {
		return nil, wrapDBErr(err, "count group members failed")
	}
	extra := `, EXISTS(SELECT 1 FROM device_group_members direct WHERE direct.tenant_id=d.tenant_id AND direct.device_id=d.id AND direct.group_id=` + origin + `::uuid),
		(SELECT jsonb_agg(m.group_id::text ORDER BY m.group_id::text) FROM device_group_members m JOIN selected_groups g ON g.id=m.group_id WHERE m.tenant_id=d.tenant_id AND m.device_id=d.id)`
	limit, offset := f.argument(query.pageSize), f.argument((query.page-1)*query.pageSize)
	rows, err := tx.QueryContext(ctx, f.cte+"SELECT "+nativeDeviceSelectFields+extra+nativeDeviceFromSQL+f.predicate()+" ORDER BY d.created_at DESC,d.id DESC LIMIT "+limit+" OFFSET "+offset, f.args...)
	if err != nil {
		return nil, wrapDBErr(err, "list group members failed")
	}
	defer rows.Close()
	for rows.Next() {
		scanner := nativeGroupMemberScanner{row: rows}
		device, err := scanNativeDeviceRow(&scanner)
		if err != nil {
			return nil, err
		}
		var matched []string
		if json.Unmarshal(scanner.matched, &matched) != nil || len(matched) == 0 {
			return nil, &errs.Error{Code: errs.Internal, Message: "invalid group member snapshot"}
		}
		if len(matched) > 1000 {
			return nil, &errs.Error{Code: errs.ResourceExhausted, Message: "matching membership set exceeds the response limit"}
		}
		page.Items = append(page.Items, NativeGroupMember{Device: *device, DirectMembership: scanner.direct, MatchedGroupIDs: matched})
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDBErr(err, "list group members failed")
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "list group members failed")
	}
	return &NativeGroupMemberPageResponse{Data: page, Meta: RequestMeta{RequestID: requestID}}, nil
}

//encore:api auth raw method=GET path=/api/v2/device-groups/:groupID/members
func (s *Service) ListNativeDeviceGroupMembers(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	if _, err := groupPrincipalForRead(); err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	groupID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("groupID"))
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	values, err := parseNativeReadValues(r, []string{"page", "pageSize", "includeDescendants", "search"})
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	query, err := decodeNativeGroupMemberQuery(values)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	result, err := readNativeGroupMemberPage(r.Context(), identity, groupID, query, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, "", result)
}
