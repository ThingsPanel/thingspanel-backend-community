package notification

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
)

func (h *ManagementHTTPHandler) providerSettings(w http.ResponseWriter, r *http.Request, id string) {
	var prefix string
	switch id {
	case "thingspanel.smtp":
		prefix = "NOTIFICATION_DEFAULT_SMTP"
	case "thingspanel.unipush":
		prefix = "NOTIFICATION_DEFAULT_UNIPUSH"
	default:
		writeManagementError(w, r, 404, ErrManagementNotFound)
		return
	}
	c, ok := h.service.pluginHTTP.(*SafePluginClient)
	cfg, err := defaultPluginConfigFromEnv(os.Getenv, prefix)
	if !ok || err != nil || !cfg.enabled || h.service.privacy == nil || h.service.privacy.Check() != nil {
		writeManagementError(w, r, 503, ErrManagementUnavailable)
		return
	}
	endpoint, err := pluginEndpoint(cfg.origin, "/v1/deployment-settings", c.options)
	if err != nil {
		writeManagementError(w, r, 503, ErrManagementUnavailable)
		return
	}
	var body []byte
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
		if err != nil || !json.Valid(body) {
			writeManagementError(w, r, 400, ErrManagementInvalid)
			return
		}
	default:
		writeManagementError(w, r, 405, ErrManagementInvalid)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint.String(), bytes.NewReader(body))
	if err != nil || setPluginBearer(req, cfg.token) != nil {
		writeManagementError(w, r, 503, ErrManagementUnavailable)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.client(endpoint)
	defer client.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		writeManagementError(w, r, 503, ErrManagementUnavailable)
		return
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 200:
	case 400, 422:
		writeManagementError(w, r, 400, ErrManagementInvalid)
		return
	case 409:
		writeManagementError(w, r, 409, ErrManagementInvalid)
		return
	default:
		writeManagementError(w, r, 503, ErrManagementUnavailable)
		return
	}
	var view struct {
		Address   string `json:"address"`
		Version   int64  `json:"version"`
		HasSecret bool   `json:"hasSecret"`
	}
	raw, err := readBounded(res.Body, 16384)
	if err != nil || json.Unmarshal(raw, &view) != nil {
		writeManagementError(w, r, 503, ErrManagementUnavailable)
		return
	}
	writeManagementSuccess(w, r, 200, view)
}
