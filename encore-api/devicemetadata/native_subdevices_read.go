package devicemetadata

import (
	"net/http"

	"encore.dev"
)

// ListNativeSubdevices returns direct children visible to the current principal.
// Parent validation, count and page are read in the same transaction.
//
//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/subdevices
func (s *Service) ListNativeSubdevices(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	id, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	values, err := parseNativeReadValues(r, []string{"page", "pageSize", "search"})
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	query, err := decodeNativeDeviceListQuery(values)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	result, err := readNativeDevicePageForGateway(r.Context(), identity, query, requestID, id)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, "", result)
}
