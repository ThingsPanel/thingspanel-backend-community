package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"encore.app/internal/authz"
	"encore.app/l1-access/authn"
	"encore.dev"
	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

type nativeDeviceReadIdentity struct {
	tenantID  string
	deviceIDs []string
}

func currentNativeDeviceReadIdentity() (nativeDeviceReadIdentity, error) {
	tenantID, err := mustTenantID()
	if err != nil {
		return nativeDeviceReadIdentity{}, err
	}
	data := authn.CurrentData()
	if !authz.Can(data, "device", "read") {
		return nativeDeviceReadIdentity{}, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to read devices"}
	}
	out := nativeDeviceReadIdentity{tenantID: tenantID}
	for _, raw := range data.AuthorizedDeviceIDs {
		id, err := nativeReadUUID(raw)
		if err != nil {
			return nativeDeviceReadIdentity{}, &errs.Error{Code: errs.Unauthenticated, Message: "invalid authenticated device scope"}
		}
		out.deviceIDs = append(out.deviceIDs, id)
	}
	return out, nil
}

func (identity nativeDeviceReadIdentity) permits(id string) bool {
	if len(identity.deviceIDs) == 0 {
		return true
	}
	for _, allowed := range identity.deviceIDs {
		if allowed == id {
			return true
		}
	}
	return false
}

type nativeDeviceListQuery struct {
	page, pageSize     int
	values             url.Values
	includeDescendants bool
	hasTemplate        *bool
}

var nativeDeviceReadQueryKeys = []string{"page", "pageSize", "search", "deviceNumber", "deviceKind", "templateId", "productId", "groupId", "includeDescendants", "gatewayId", "administrativeState", "activationState", "connectivity", "label", "sort", "groupScope", "accessWay", "protocol", "providerId", "serviceInstanceId", "firmwareVersion", "batchNumber", "alarmState", "modelId", "hasTemplate", "gatewayScope"}

func parseNativeReadValues(r *http.Request, keys []string) (url.Values, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, invalidGroupQuery("malformed query string")
	}
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return nil, invalidGroupQuery("unsupported or repeated query parameter: " + key)
		}
		if !utf8.ValidString(entries[0]) {
			return nil, invalidGroupQuery("invalid UTF-8 query parameter: " + key)
		}
	}
	return values, nil
}

func nativeReadUUID(raw string) (string, error) {
	id, err := uuid.Parse(raw)
	if err != nil || len(raw) != 36 || id == uuid.Nil {
		return "", invalidGroupQuery("expected a UUID")
	}
	return id.String(), nil
}

func decodeNativeDeviceListQuery(values url.Values) (nativeDeviceListQuery, error) {
	out := nativeDeviceListQuery{values: values}
	var err error
	for _, name := range []string{"page", "pageSize"} {
		if raw, ok := values[name]; ok && raw[0] == "" {
			return out, invalidGroupQuery(name + " must not be empty")
		}
	}
	out.page, out.pageSize, err = normalizeDeviceGroupPagination(values.Get("page"), values.Get("pageSize"))
	if err != nil {
		return out, err
	}
	for key, max := range map[string]int{"search": 128, "deviceNumber": 128, "label": 322, "protocol": 64, "serviceInstanceId": 256, "firmwareVersion": 128, "batchNumber": 128} {
		if utf8.RuneCountInString(values.Get(key)) > max || (key == "deviceNumber" && values.Has(key) && values.Get(key) == "") {
			return out, invalidGroupQuery("invalid " + key)
		}
	}
	for key, choices := range map[string][]string{
		"deviceKind": {"standalone", "gateway", "subdevice"}, "administrativeState": {"enabled", "disabled"}, "activationState": {"inactive", "active"},
		"connectivity": {"online", "offline", "unknown"}, "sort": {"createdAt", "-createdAt", "name", "-name", "deviceNumber", "-deviceNumber"},
		"groupScope": {"all", "ungrouped"}, "accessWay": {"protocol", "service"}, "alarmState": {"any", "active", "none"}, "gatewayScope": {"all", "bound", "unbound"},
	} {
		if !values.Has(key) {
			continue
		}
		found := false
		for _, choice := range choices {
			if values.Get(key) == choice {
				found = true
				break
			}
		}
		if !found {
			return out, invalidGroupQuery("invalid " + key)
		}
	}
	for _, key := range []string{"templateId", "productId", "groupId", "gatewayId", "providerId", "modelId"} {
		if values.Has(key) {
			id, err := nativeReadUUID(values.Get(key))
			if err != nil {
				return out, invalidGroupQuery("invalid " + key)
			}
			values.Set(key, id)
		}
	}
	for _, key := range []string{"includeDescendants", "hasTemplate"} {
		if !values.Has(key) {
			continue
		}
		if values.Get(key) != "true" && values.Get(key) != "false" {
			return out, invalidGroupQuery(key + " must be true or false")
		}
		value := values.Get(key) == "true"
		if key == "includeDescendants" {
			out.includeDescendants = value
		} else {
			out.hasTemplate = &value
		}
	}
	if label := values.Get("label"); label != "" {
		parts := strings.SplitN(label, "=", 2)
		if len(parts) != 2 || !deviceLabelKeyPattern.MatchString(parts[0]) || utf8.RuneCountInString(parts[1]) > 256 {
			return out, invalidGroupQuery("label must contain one valid key=value pair")
		}
	}
	return out, nil
}

type nativeDeviceReadFilter struct {
	where []string
	args  []any
	cte   string
}

func (f *nativeDeviceReadFilter) argument(value any) string {
	f.args = append(f.args, value)
	return "$" + strconv.Itoa(len(f.args))
}
func (f *nativeDeviceReadFilter) predicate() string {
	return " WHERE " + strings.Join(f.where, " AND ")
}
func nativeDeviceBaseFilter(identity nativeDeviceReadIdentity) nativeDeviceReadFilter {
	f := nativeDeviceReadFilter{where: []string{"d.tenant_id=$1", "d.deleted_at IS NULL"}, args: []any{identity.tenantID}}
	if len(identity.deviceIDs) > 0 {
		f.where = append(f.where, "d.id=ANY(string_to_array("+f.argument(strings.Join(identity.deviceIDs, ","))+",',')::uuid[])")
	}
	return f
}

func nativeGroupSelectionCTE(groupPlaceholder string, descendants bool) string {
	if !descendants {
		return "WITH selected_groups AS (SELECT id FROM device_groups WHERE tenant_id=$1 AND deleted_at IS NULL AND id=" + groupPlaceholder + "::uuid) "
	}
	// UNION deduplicates visited IDs, so a corrupt historical cycle cannot make
	// an unbounded recursive walk. All hierarchy rows remain tenant/live scoped.
	return "WITH RECURSIVE selected_groups(id) AS (SELECT id FROM device_groups WHERE tenant_id=$1 AND deleted_at IS NULL AND id=" + groupPlaceholder + "::uuid UNION SELECT g.id FROM device_groups g JOIN selected_groups p ON g.parent_id=p.id WHERE g.tenant_id=$1 AND g.deleted_at IS NULL) "
}

func addNativeDeviceFilters(filter *nativeDeviceReadFilter, query nativeDeviceListQuery) {
	v := query.values
	for _, field := range []struct{ key, column string }{
		{"deviceNumber", "d.device_number"}, {"deviceKind", "d.device_kind"}, {"productId", "d.product_id"}, {"gatewayId", "gm.gateway_device_id"},
		{"administrativeState", "d.administrative_state"}, {"activationState", "d.activation_state"}, {"batchNumber", "d.batch_number"}, {"templateId", "b.template_id"}, {"modelId", "t.thing_model_id"},
	} {
		if v.Has(field.key) {
			filter.where = append(filter.where, field.column+"="+filter.argument(v.Get(field.key)))
		}
	}
	if search := v.Get("search"); search != "" {
		p := filter.argument(search)
		filter.where = append(filter.where, "(strpos(d.name,"+p+")>0 OR strpos(d.device_number,"+p+")>0)")
	}
	if label := v.Get("label"); label != "" {
		parts := strings.SplitN(label, "=", 2)
		encoded, _ := json.Marshal(map[string]string{parts[0]: parts[1]})
		filter.where = append(filter.where, "d.labels @> "+filter.argument(string(encoded))+"::jsonb")
	}
	if v.Get("groupScope") == "ungrouped" {
		filter.where = append(filter.where, "NOT EXISTS(SELECT 1 FROM device_group_members m JOIN device_groups g ON g.tenant_id=m.tenant_id AND g.id=m.group_id WHERE m.tenant_id=d.tenant_id AND m.device_id=d.id AND g.deleted_at IS NULL)")
	}
	if group := v.Get("groupId"); group != "" {
		filter.cte = nativeGroupSelectionCTE(filter.argument(group), query.includeDescendants)
		filter.where = append(filter.where, "EXISTS(SELECT 1 FROM device_group_members m JOIN selected_groups g ON g.id=m.group_id WHERE m.tenant_id=d.tenant_id AND m.device_id=d.id)")
	}
	if query.hasTemplate != nil {
		if *query.hasTemplate {
			filter.where = append(filter.where, "b.template_id IS NOT NULL")
		} else {
			filter.where = append(filter.where, "b.template_id IS NULL")
		}
	}
	if v.Get("gatewayScope") == "bound" {
		filter.where = append(filter.where, "gm.child_device_id IS NOT NULL")
	}
	if v.Get("gatewayScope") == "unbound" {
		filter.where = append(filter.where, "gm.child_device_id IS NULL")
	}
	access := "COALESCE(t.configuration->'access',b.direct_access->'access')"
	for _, field := range []struct{ key, property string }{{"protocol", "protocol"}, {"serviceInstanceId", "serviceInstanceId"}, {"providerId", "providerId"}} {
		if v.Has(field.key) {
			expr := "(" + access + "->>'" + field.property + "')"
			if field.key == "providerId" {
				expr = "lower(" + expr + ")"
			}
			filter.where = append(filter.where, expr+"="+filter.argument(v.Get(field.key)))
		}
	}
	if v.Get("accessWay") == "service" {
		filter.where = append(filter.where, "("+access+"->>'mode')='service_connector'")
	}
	if v.Get("accessWay") == "protocol" {
		filter.where = append(filter.where, "("+access+"->>'mode') IN ('direct','protocol_connector','gateway')")
	}
}

func requireNativeDeviceReadiness(ctx context.Context, tx *sql.Tx, identity nativeDeviceReadIdentity) error {
	filter := nativeDeviceBaseFilter(identity)
	var incomplete bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 "+nativeDeviceFromSQL+filter.predicate()+` AND (d.device_number IS NULL OR d.name IS NULL OR d.device_kind IS NULL OR d.activation_state IS NULL OR d.created_by IS NULL OR d.updated_by IS NULL OR b.device_id IS NULL))`, filter.args...).Scan(&incomplete)
	if err != nil {
		return wrapDBErr(err, "device readiness check failed")
	}
	if incomplete {
		return &errs.Error{Code: errs.Unavailable, Message: "device metadata offline conversion is incomplete"}
	}
	return nil
}

func requireNativeGroupRead(ctx context.Context, tx *sql.Tx, tenantID, groupID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id::text FROM device_groups WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, groupID).Scan(&id)
	if err == sql.ErrNoRows {
		return &errs.Error{Code: errs.NotFound, Message: "device group not found"}
	}
	if err != nil {
		return wrapDBErr(err, "read group failed")
	}
	return nil
}

type NativeDevicePage struct {
	Items    []NativeDevice `json:"items"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Total    int64          `json:"total"`
}
type NativeDevicePageResponse struct {
	Data NativeDevicePage `json:"data"`
	Meta RequestMeta      `json:"meta"`
}
type NativeDeviceResponse struct {
	Data NativeDevice `json:"data"`
	Meta RequestMeta  `json:"meta"`
}

func readNativeDevicePage(ctx context.Context, identity nativeDeviceReadIdentity, query nativeDeviceListQuery, requestID string) (*NativeDevicePageResponse, error) {
	return readNativeDevicePageForGateway(ctx, identity, query, requestID, "")
}

func readNativeDevicePageForGateway(ctx context.Context, identity nativeDeviceReadIdentity, query nativeDeviceListQuery, requestID, gatewayID string) (*NativeDevicePageResponse, error) {
	// Runtime/OTA/alarm observations must be resolved BEFORE pagination. Until
	// their authoritative snapshot adapter is connected, fail closed rather
	// than silently filtering one page or interpreting an outage as zero rows.
	if query.values.Has("connectivity") || query.values.Has("firmwareVersion") || (query.values.Has("alarmState") && query.values.Get("alarmState") != "any") {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "authoritative runtime filter snapshot is unavailable"}
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "list devices failed")
	}
	defer tx.Rollback()
	if gatewayID != "" {
		if !identity.permits(gatewayID) {
			return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
		}
		gateway, err := getNativeDeviceRecordTx(ctx, tx, identity.tenantID, gatewayID)
		if err != nil {
			return nil, err
		}
		if gateway.DeviceKind != "gateway" {
			return nil, &errs.Error{Code: errs.FailedPrecondition, Message: "device is not a gateway"}
		}
		query.values.Set("gatewayId", gatewayID)
	}
	if group := query.values.Get("groupId"); group != "" {
		if err := requireNativeGroupRead(ctx, tx, identity.tenantID, group); err != nil {
			return nil, err
		}
	}
	if err := requireNativeDeviceReadiness(ctx, tx, identity); err != nil {
		return nil, err
	}
	f := nativeDeviceBaseFilter(identity)
	addNativeDeviceFilters(&f, query)
	page := NativeDevicePage{Items: []NativeDevice{}, Page: query.page, PageSize: query.pageSize}
	if err := tx.QueryRowContext(ctx, f.cte+"SELECT count(*) "+nativeDeviceFromSQL+f.predicate(), f.args...).Scan(&page.Total); err != nil {
		return nil, wrapDBErr(err, "count devices failed")
	}
	order := map[string]string{"createdAt": "d.created_at ASC,d.id ASC", "-createdAt": "d.created_at DESC,d.id DESC", "name": "d.name ASC,d.id ASC", "-name": "d.name DESC,d.id ASC", "deviceNumber": "d.device_number ASC,d.id ASC", "-deviceNumber": "d.device_number DESC,d.id ASC"}[query.values.Get("sort")]
	if order == "" {
		order = "d.created_at DESC,d.id DESC"
	}
	limit, offset := f.argument(query.pageSize), f.argument((query.page-1)*query.pageSize)
	rows, err := tx.QueryContext(ctx, f.cte+"SELECT "+nativeDeviceSelectFields+nativeDeviceFromSQL+f.predicate()+" ORDER BY "+order+" LIMIT "+limit+" OFFSET "+offset, f.args...)
	if err != nil {
		return nil, wrapDBErr(err, "list devices failed")
	}
	defer rows.Close()
	for rows.Next() {
		device, err := scanNativeDeviceRow(rows)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, *device)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDBErr(err, "list devices failed")
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, wrapDBErr(err, "list devices failed")
	}
	return &NativeDevicePageResponse{Data: page, Meta: RequestMeta{RequestID: requestID}}, nil
}

//encore:api auth raw method=GET path=/api/v2/devices
func (s *Service) ListNativeDevices(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	values, err := parseNativeReadValues(r, nativeDeviceReadQueryKeys)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	query, err := decodeNativeDeviceListQuery(values)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	result, err := readNativeDevicePage(r.Context(), identity, query, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, "", result)
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID
func (s *Service) GetNativeDevice(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	id, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if !identity.permits(id) {
		writeGroupReadProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.NotFound, Message: "device not found"})
		return
	}
	tx, err := beginTenantGroupTx(r.Context(), identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, wrapDBErr(err, "get device failed"))
		return
	}
	defer tx.Rollback()
	device, err := getNativeDeviceRecordTx(r.Context(), tx, identity.tenantID, id)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, wrapDBErr(err, "get device failed"))
		return
	}
	etag, err := nativeDeviceETag(device)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.Internal, Message: "failed to encode device"})
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, etag, NativeDeviceResponse{Data: *device, Meta: RequestMeta{RequestID: requestID}})
}
