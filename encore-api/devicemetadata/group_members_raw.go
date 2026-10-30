package devicemetadata

import (
	"errors"
	"io"
	"net/http"

	"encore.dev"
	"encore.dev/beta/errs"
)

//encore:api auth raw method=POST path=/api/v2/device-groups/:groupID/member-additions
func (s *Service) AddDeviceGroupMembers(w http.ResponseWriter, r *http.Request) {
	s.setDeviceGroupMembers(w, r, addDeviceGroupMembersOperation)
}

//encore:api auth raw method=POST path=/api/v2/device-groups/:groupID/member-removals
func (s *Service) RemoveDeviceGroupMembers(w http.ResponseWriter, r *http.Request) {
	s.setDeviceGroupMembers(w, r, removeDeviceGroupMembersOperation)
}

func (s *Service) setDeviceGroupMembers(w http.ResponseWriter, r *http.Request, operation string) {
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
	req, err := decodeSetGroupMembersBody(body)
	if err != nil {
		status := 0
		var shapeErr *setGroupMembersBodySyntaxError
		if errors.As(err, &shapeErr) {
			status = http.StatusBadRequest
		}
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, status)
		return
	}
	groupID := encore.CurrentRequest().PathParams.Get("groupID")
	groupID, err = normalizeDeviceGroupID(groupID, false)
	if err != nil {
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	result, err := setDeviceGroupMembersIdempotently(r.Context(), principal, groupID, key, req, operation, requestID)
	if err != nil {
		status := 0
		switch errs.Code(err) {
		case errs.FailedPrecondition, errs.AlreadyExists:
			status = http.StatusConflict
		case errs.InvalidArgument:
			status = http.StatusUnprocessableEntity
		}
		writeGroupUpdateProblem(w, requestID, r.URL.Path, err, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", result.requestID)
	w.WriteHeader(result.status)
	_, _ = w.Write(result.response)
}
