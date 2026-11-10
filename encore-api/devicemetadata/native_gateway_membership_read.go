package devicemetadata

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"encore.dev"
	"encore.dev/beta/errs"
)

// NativeGatewayMembership is the current parent relationship for a device.
// A detached device is represented by null gatewayId and subdeviceAddress.
type NativeGatewayMembership struct {
	SubdeviceID      string  `json:"subdeviceId"`
	GatewayID        *string `json:"gatewayId"`
	SubdeviceAddress *string `json:"subdeviceAddress"`
	ResourceVersion  int64   `json:"resourceVersion"`
}

type NativeGatewayMembershipResponse struct {
	Data NativeGatewayMembership `json:"data"`
	Meta RequestMeta             `json:"meta"`
}

func readNativeGatewayMembership(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, requestID string) (*NativeGatewayMembershipResponse, string, error) {
	if !identity.permits(deviceID) {
		return nil, "", &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, "", wrapDBErr(err, "read gateway membership failed")
	}
	defer tx.Rollback()

	device, err := getNativeDeviceRecordTx(ctx, tx, identity.tenantID, deviceID)
	if err != nil {
		return nil, "", err
	}
	if device.ResourceVersion < 1 || device.ResourceVersion > 9007199254740991 {
		return nil, "", &errs.Error{Code: errs.Unavailable, Message: "device resource version is outside the supported range"}
	}
	result := NativeGatewayMembership{
		SubdeviceID: device.ID, GatewayID: nil, SubdeviceAddress: nil,
		ResourceVersion: device.ResourceVersion,
	}
	var gatewayID, address, gatewayKind sql.NullString
	var gatewayLive sql.NullBool
	err = tx.QueryRowContext(ctx, `
		SELECT gm.gateway_device_id::text,gm.subdevice_address,gateway.device_kind,gateway.deleted_at IS NULL
		FROM gateway_memberships gm
		JOIN devices child ON child.tenant_id=gm.tenant_id AND child.id=gm.child_device_id
		LEFT JOIN devices gateway ON gateway.tenant_id=gm.tenant_id AND gateway.id=gm.gateway_device_id
		WHERE gm.tenant_id=$1 AND gm.child_device_id=$2 AND child.deleted_at IS NULL`, identity.tenantID, deviceID).Scan(&gatewayID, &address, &gatewayKind, &gatewayLive)
	if err != nil && err != sql.ErrNoRows {
		return nil, "", wrapDBErr(err, "read gateway membership failed")
	}
	if err == nil {
		if !gatewayID.Valid || gatewayID.String == "" || !address.Valid || address.String == "" ||
			!gatewayKind.Valid || gatewayKind.String != "gateway" || !gatewayLive.Valid || !gatewayLive.Bool {
			return nil, "", &errs.Error{Code: errs.Unavailable, Message: "gateway membership is incomplete"}
		}
		result.GatewayID = &gatewayID.String
		result.SubdeviceAddress = &address.String
	}
	if err := tx.Commit(); err != nil {
		return nil, "", wrapDBErr(err, "read gateway membership failed")
	}
	response := &NativeGatewayMembershipResponse{Data: result, Meta: RequestMeta{RequestID: requestID}}
	encoded, err := json.Marshal(response.Data)
	if err != nil {
		return nil, "", &errs.Error{Code: errs.Internal, Message: "failed to encode gateway membership"}
	}
	sum := sha256.Sum256(encoded)
	return response, `"sha256-` + hex.EncodeToString(sum[:]) + `"`, nil
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/gateway-membership
func (s *Service) GetNativeGatewayMembership(w http.ResponseWriter, r *http.Request) {
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
	deviceID, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("deviceID"))
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	response, etag, err := readNativeGatewayMembership(r.Context(), identity, deviceID, requestID)
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, etag, response)
}
