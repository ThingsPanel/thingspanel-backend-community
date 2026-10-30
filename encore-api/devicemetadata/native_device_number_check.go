package devicemetadata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"unicode"
	"unicode/utf8"

	"encore.app/internal/authz"
	"encore.app/internal/nativehttp"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
)

// Device number pre-check (DM-DEV-008). It is advice for forms only: the unique
// index uq_devices_tenant_device_number stays the real guard. The index also
// covers soft-deleted devices, so a deleted device still reserves its number and
// this check counts it too.

type deviceNumberCheckInput struct {
	DeviceNumber    string  `json:"deviceNumber"`
	ExcludeDeviceID *string `json:"excludeDeviceId"`
}

type DeviceNumberAvailability struct {
	DeviceNumber string `json:"deviceNumber"`
	Available    bool   `json:"available"`
}

type DeviceNumberAvailabilityResponse struct {
	Data DeviceNumberAvailability `json:"data"`
	Meta RequestMeta              `json:"meta"`
}

func decodeDeviceNumberCheck(body []byte) (deviceNumberCheckInput, error) {
	var in deviceNumberCheckInput
	if err := decodeStrict(body, &in); err != nil {
		return in, err
	}
	n := utf8.RuneCountInString(in.DeviceNumber)
	if n < 1 || n > 128 || !utf8.ValidString(in.DeviceNumber) {
		return in, invalidStructure("deviceNumber must be 1 to 128 characters")
	}
	for _, r := range in.DeviceNumber {
		if unicode.IsControl(r) {
			return in, invalidStructure("deviceNumber must not contain control characters")
		}
	}
	if in.DeviceNumber != string([]rune(in.DeviceNumber)) || in.DeviceNumber[0] == ' ' || in.DeviceNumber[len(in.DeviceNumber)-1] == ' ' {
		return in, invalidStructure("deviceNumber must not start or end with a space")
	}
	if in.ExcludeDeviceID != nil {
		if _, err := nativeReadUUID(*in.ExcludeDeviceID); err != nil {
			return in, invalidStructure("excludeDeviceId must be a UUID or null")
		}
	}
	return in, nil
}

func checkNativeDeviceNumber(ctx context.Context, tenantID string, in deviceNumberCheckInput) (DeviceNumberAvailability, error) {
	var taken bool
	err := db.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM devices WHERE tenant_id=$1 AND device_number=$2 AND ($3::uuid IS NULL OR id <> $3::uuid))`,
		tenantID, in.DeviceNumber, in.ExcludeDeviceID).Scan(&taken)
	if err != nil {
		return DeviceNumberAvailability{}, wrapDBErr(err, "check device number failed")
	}
	return DeviceNumberAvailability{DeviceNumber: in.DeviceNumber, Available: !taken}, nil
}

//encore:api auth raw method=POST path=/api/v2/device-number-validations
func (s *Service) CheckNativeDeviceNumber(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeDeviceReadIdentity()
	if err == nil && !authz.Can(authn.CurrentData(), "device", "read") {
		err = &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to read devices"}
	}
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "request body is missing or too large"}, 0, false)
		return
	}
	input, err := decodeDeviceNumberCheck(body)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	result, err := checkNativeDeviceNumber(r.Context(), identity.tenantID, input)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	payload, _ := json.Marshal(DeviceNumberAvailabilityResponse{Data: result, Meta: RequestMeta{RequestID: requestID}})
	w.Header().Set("Cache-Control", "no-store")
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", json.RawMessage(payload))
}
