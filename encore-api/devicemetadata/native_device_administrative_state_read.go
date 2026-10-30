package devicemetadata

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
)

// NativeAdministrativeState is the device's management intent. It is kept
// separate from activation and observed connectivity state.
type NativeAdministrativeState struct {
	State           string `json:"state"`
	ResourceVersion int64  `json:"resourceVersion"`
}

type NativeAdministrativeStateResponse struct {
	Data NativeAdministrativeState `json:"data"`
	Meta RequestMeta               `json:"meta"`
}

func nativeAdministrativeStateETag(state NativeAdministrativeState) (string, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return `"sha256-` + hex.EncodeToString(sum[:]) + `"`, nil
}

func readNativeAdministrativeState(r *http.Request, identity nativeDeviceReadIdentity, deviceID, requestID string) (*NativeAdministrativeStateResponse, string, error) {
	if !identity.permits(deviceID) {
		return nil, "", &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}

	tx, err := beginTenantGroupTx(r.Context(), identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, "", wrapDBErr(err, "read device administrative state failed")
	}
	defer tx.Rollback()

	device, err := getNativeDeviceRecordTx(r.Context(), tx, identity.tenantID, deviceID)
	if err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", wrapDBErr(err, "read device administrative state failed")
	}

	state := NativeAdministrativeState{State: device.AdministrativeState, ResourceVersion: device.ResourceVersion}
	etag, err := nativeAdministrativeStateETag(state)
	if err != nil {
		return nil, "", &errs.Error{Code: errs.Internal, Message: "failed to encode device administrative state"}
	}
	return &NativeAdministrativeStateResponse{Data: state, Meta: RequestMeta{RequestID: requestID}}, etag, nil
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/administrative-state
func (s *Service) GetDeviceAdministrativeState(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	response, etag, err := readNativeAdministrativeState(r, identity, deviceID, requestID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, etag, response)
}
