package devicemetadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"encore.app/internal/nativehttp"
	"encore.dev"
	"encore.dev/beta/errs"
)

// connectionInfoEndpointEnv names the device-facing endpoint operators publish to
// devices (for example mqtts://iot.example.com:8883). It is deployment
// configuration, not device data, so an unset value is reported as null rather
// than guessed. Secrets and authentication packets are never part of this answer.
const connectionInfoEndpointEnv = "TP_DEVICE_CONNECTION_ENDPOINT"

type NativeConnectionInfo struct {
	DeviceID         string  `json:"deviceId"`
	Protocol         string  `json:"protocol"`
	Endpoint         *string `json:"endpoint"`
	ClientIdentifier *string `json:"clientIdentifier"`
	GatewayID        *string `json:"gatewayId"`
	ObservedAt       *string `json:"observedAt"`
}

type NativeConnectionInfoResponse struct {
	Data NativeConnectionInfo `json:"data"`
	Meta RequestMeta          `json:"meta"`
}

// accessProtocol finds the protocol in either the template's current configuration
// ({"access":{"protocol":...}}) or a direct-access object ({"protocol":...}).
func accessProtocol(documents ...[]byte) string {
	for _, raw := range documents {
		if len(raw) == 0 {
			continue
		}
		var doc struct {
			Protocol string `json:"protocol"`
			Access   *struct {
				Protocol string `json:"protocol"`
			} `json:"access"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		if p := strings.TrimSpace(doc.Protocol); p != "" {
			return p
		}
		if doc.Access != nil {
			if p := strings.TrimSpace(doc.Access.Protocol); p != "" {
				return p
			}
		}
	}
	return ""
}

func readNativeConnectionInfo(ctx context.Context, identity nativeDeviceReadIdentity, deviceID, requestID string) (*NativeConnectionInfoResponse, error) {
	if !identity.permits(deviceID) {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device not found"}
	}
	tx, err := beginTenantGroupTx(ctx, identity.tenantID, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, wrapDBErr(err, "read device connection info failed")
	}
	defer tx.Rollback()
	device, err := getNativeDeviceRecordTx(ctx, tx, identity.tenantID, deviceID)
	if err != nil {
		return nil, err
	}
	var direct, templateConfig []byte
	err = tx.QueryRowContext(ctx, `SELECT b.direct_access::text, t.configuration::text
		FROM device_access_bindings b
		LEFT JOIN device_access_templates t ON t.tenant_id=b.tenant_id AND t.id=b.template_id AND t.deleted_at IS NULL
		WHERE b.tenant_id=$1 AND b.device_id=$2`, identity.tenantID, deviceID).Scan(&direct, &templateConfig)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, wrapDBErr(err, "read device access binding failed")
	}
	protocol := accessProtocol(templateConfig, direct)
	if protocol == "" {
		return nil, &errs.Error{Code: errs.NotFound, Message: "device access is not configured"}
	}
	info := NativeConnectionInfo{DeviceID: deviceID, Protocol: protocol, GatewayID: device.GatewayID}
	if endpoint := strings.TrimSpace(os.Getenv(connectionInfoEndpointEnv)); endpoint != "" && len(endpoint) <= 1024 {
		info.Endpoint = &endpoint
	}
	return &NativeConnectionInfoResponse{Data: info, Meta: RequestMeta{RequestID: requestID}}, nil
}

//encore:api auth raw method=GET path=/api/v2/devices/:deviceID/connection-info
func (s *Service) GetNativeDeviceConnectionInfo(w http.ResponseWriter, r *http.Request) {
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
	response, err := readNativeConnectionInfo(r.Context(), identity, deviceID, requestID)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, false)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}
