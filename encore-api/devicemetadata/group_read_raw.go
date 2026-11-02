package devicemetadata

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
)

// GroupProblem is the public problem envelope for device-group read routes.
type GroupProblem = nativehttp.Problem

//encore:api auth raw method=GET path=/api/v2/device-groups
func (s *Service) ListDeviceGroups(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	query, err := parseGroupReadQuery(r, "page", "pageSize", "parentId", "rootOnly", "search", "sort")
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	rootOnly := false
	if raw, ok := query["rootOnly"]; ok {
		if raw[0] != "true" && raw[0] != "false" {
			writeGroupReadProblem(w, requestID, r.URL.Path, invalidGroupQuery("rootOnly must be true or false"))
			return
		}
		rootOnly = raw[0] == "true"
	}
	req := &ListDeviceGroupsReq{
		Page: query.Get("page"), PageSize: query.Get("pageSize"), ParentID: query.Get("parentId"),
		RootOnly: rootOnly, Search: query.Get("search"), Sort: query.Get("sort"),
	}
	response, err := s.listDeviceGroups(r.Context(), req, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}

//encore:api auth raw method=GET path=/api/v2/device-groups/:groupID
func (s *Service) GetDeviceGroup(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	if _, err := parseGroupReadQuery(r); err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	groupID := encore.CurrentRequest().PathParams.Get("groupID")
	response, err := s.getDeviceGroup(r.Context(), groupID, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, response.ETag, response)
}

//encore:api auth raw method=GET path=/api/v2/device-group-tree
func (s *Service) GetDeviceGroupTree(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	query, err := parseGroupReadQuery(r, "rootId", "maxDepth")
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	maxDepth := ""
	if raw, ok := query["maxDepth"]; ok {
		if _, err := strconv.Atoi(raw[0]); err != nil {
			writeGroupReadProblem(w, requestID, r.URL.Path, invalidGroupQuery("maxDepth must be an integer between 1 and 32"))
			return
		}
		maxDepth = raw[0]
	}
	req := &DeviceGroupTreeReq{RootID: query.Get("rootId"), MaxDepth: maxDepth}
	response, err := s.getDeviceGroupTree(r.Context(), req, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, response.ETag, response)
}

//encore:api auth raw method=POST path=/api/v2/device-groups
func (s *Service) CreateDeviceGroup(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	principal, err := groupPrincipalForWrite()
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, 0)
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	key := r.Header.Get("Idempotency-Key")
	if len(keys) != 1 || !validIdempotencyKey(key) {
		writeGroupCreateProblem(w, requestID, r.URL.Path, invalidGroupQuery("Idempotency-Key must be 8 to 128 ASCII letters, digits, or . _ : - characters"), http.StatusBadRequest)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	var req CreateDeviceGroupReq
	if err := decoder.Decode(&req); err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, invalidGroupQuery("request body must be a valid CreateDeviceGroup object"), http.StatusBadRequest)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeGroupCreateProblem(w, requestID, r.URL.Path, invalidGroupQuery("request body must contain one JSON object"), http.StatusBadRequest)
		return
	}
	result, err := createDeviceGroupIdempotently(r.Context(), principal, key, &req, requestID)
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, 0)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", result.requestID)
	w.Header().Set("Location", result.location)
	w.WriteHeader(result.status)
	_, _ = w.Write(result.response)
}

//encore:api auth raw method=PATCH path=/api/v2/device-groups/:groupID
func (s *Service) UpdateDeviceGroup(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	principal, err := groupPrincipalForWrite()
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, 0)
		return
	}
	ifMatchValues := r.Header.Values("If-Match")
	if len(ifMatchValues) == 0 {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.FailedPrecondition, Message: "If-Match is required"}, http.StatusPreconditionRequired)
		return
	}
	if len(ifMatchValues) != 1 || !validDeviceGroupIfMatch(ifMatchValues[0]) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("If-Match must contain one strong device-group ETag"), http.StatusBadRequest)
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	key := r.Header.Get("Idempotency-Key")
	if len(keys) != 1 || !validIdempotencyKey(key) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("Idempotency-Key must be 8 to 128 ASCII letters, digits, or . _ : - characters"), http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16*1024))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("request body is too large or unreadable"), http.StatusBadRequest)
		return
	}
	req, err := decodeUpdateDeviceGroupBody(body)
	if err != nil {
		statusOverride := 0
		var shapeErr *updateDeviceGroupBodySyntaxError
		if errors.As(err, &shapeErr) {
			statusOverride = http.StatusBadRequest
		}
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, statusOverride)
		return
	}
	groupID := encore.CurrentRequest().PathParams.Get("groupID")
	groupID, err = normalizeDeviceGroupID(groupID, false)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := updateDeviceGroupIdempotently(r.Context(), principal, groupID, key, ifMatchValues[0], req, requestID)
	if err != nil {
		statusOverride := 0
		switch errs.Code(err) {
		case errs.FailedPrecondition:
			statusOverride = http.StatusPreconditionFailed
		case errs.InvalidArgument:
			statusOverride = http.StatusUnprocessableEntity
		}
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, statusOverride)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", result.requestID)
	w.Header().Set("ETag", result.etag)
	w.WriteHeader(result.status)
	_, _ = w.Write(result.response)
}

//encore:api auth raw method=POST path=/api/v2/device-groups/:groupID/moves
func (s *Service) MoveDeviceGroup(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	principal, err := groupPrincipalForWrite()
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, 0)
		return
	}
	keys := r.Header.Values("Idempotency-Key")
	key := r.Header.Get("Idempotency-Key")
	if len(keys) != 1 || !validIdempotencyKey(key) {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("Idempotency-Key must be 8 to 128 ASCII letters, digits, or . _ : - characters"), http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16*1024))
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, invalidGroupQuery("request body is too large or unreadable"), http.StatusBadRequest)
		return
	}
	req, err := decodeMoveDeviceGroupBody(body)
	if err != nil {
		statusOverride := 0
		var shapeErr *moveDeviceGroupBodySyntaxError
		if errors.As(err, &shapeErr) {
			statusOverride = http.StatusBadRequest
		}
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, statusOverride)
		return
	}
	groupID := encore.CurrentRequest().PathParams.Get("groupID")
	groupID, err = normalizeDeviceGroupID(groupID, false)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := moveDeviceGroupIdempotently(r.Context(), principal, groupID, key, req, requestID)
	if err != nil {
		statusOverride := 0
		switch errs.Code(err) {
		case errs.FailedPrecondition, errs.AlreadyExists:
			statusOverride = http.StatusConflict
		case errs.InvalidArgument:
			statusOverride = http.StatusUnprocessableEntity
		}
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, statusOverride)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", result.requestID)
	w.WriteHeader(result.status)
	_, _ = w.Write(result.response)
}

func parseGroupReadQuery(r *http.Request, allowed ...string) (url.Values, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, invalidGroupQuery("malformed query string")
	}
	allowlist := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowlist[key] = struct{}{}
	}
	for key, items := range values {
		if _, ok := allowlist[key]; !ok {
			return nil, invalidGroupQuery("unsupported query parameter")
		}
		if len(items) != 1 {
			return nil, invalidGroupQuery("query parameter must not be repeated: " + key)
		}
		if items[0] == "" || (key == "search" && strings.TrimSpace(items[0]) == "") {
			return nil, invalidGroupQuery("query parameter must not be empty: " + key)
		}
	}
	return values, nil
}

func invalidGroupQuery(message string) error {
	return &errs.Error{Code: errs.InvalidArgument, Message: message}
}

func writeGroupReadJSON(w http.ResponseWriter, requestID, instance string, status int, etag string, payload any) {
	nativehttp.WriteJSON(w, requestID, instance, status, etag, payload)
}

func writeGroupReadProblem(w http.ResponseWriter, requestID, instance string, err error) {
	writeGroupProblem(w, requestID, instance, err, 0, false)
}

func writeGroupCreateProblem(w http.ResponseWriter, requestID, instance string, err error, statusOverride int) {
	writeGroupProblem(w, requestID, instance, err, statusOverride, true)
}

func writeGroupUpdateProblem(w http.ResponseWriter, requestID, instance string, err error, statusOverride int) {
	writeGroupProblem(w, requestID, instance, err, statusOverride, true)
}

func writeGroupProblem(w http.ResponseWriter, requestID, instance string, err error, statusOverride int, invalidAs422 bool) {
	nativehttp.WriteProblem(w, requestID, instance, err, statusOverride, invalidAs422)
}
