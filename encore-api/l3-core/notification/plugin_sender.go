package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"time"

	"encore.app/l3-core/notification/contract"
)

// InstancePluginSender resolves the exact claimed configuration's registration.
// A failed local lookup does not authorize another provider attempt.
type InstancePluginSender struct {
	Client   *SafePluginClient
	Runtimes PluginRuntimeResolver
}

func (s *InstancePluginSender) Send(ctx context.Context, request contract.PluginSendRequestV1) (contract.PluginSendResultV1, error) {
	if s == nil || s.Client == nil || s.Runtimes == nil {
		return uncertainPluginSubmission(request.DeliveryID), nil
	}
	runtime, err := s.Runtimes.ResolvePluginRuntime(ctx, request.TenantID, request.InstanceID, request.ConfigVersion)
	if err != nil {
		return uncertainPluginSubmission(request.DeliveryID), nil
	}
	return s.Client.SendInstance(ctx, runtime, request), nil
}

func uncertainPluginSubmission(deliveryID string) contract.PluginSendResultV1 {
	return contract.PluginSendResultV1{DeliveryID: deliveryID, Outcome: "unknown", ReceiptExpected: false, Error: &contract.PluginDeliveryErrorV1{Code: "submission_uncertain", Message: "Submission outcome is unknown; automatic retry is disabled.", Retryable: false}}
}

// SendInstance performs one HTTP submission. Disable connection reuse and HTTP/2
// retry handling; never forward an idempotency header or provide GetBody for an
// automatic transport replay. Redirects, non-200 responses and malformed JSON
// all become unknown, regardless of a provider-looking error string.
func (c *SafePluginClient) SendInstance(ctx context.Context, runtime PluginRuntime, request contract.PluginSendRequestV1) contract.PluginSendResultV1 {
	unknown := uncertainPluginSubmission(request.DeliveryID)
	if c == nil {
		return unknown
	}
	deadline, err := time.Parse(time.RFC3339Nano, request.ExpiresAt)
	if err != nil || !deadline.After(time.Now()) {
		return unknown
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	endpoint, err := pluginEndpoint(runtime.Origin, "/v1/send", c.options)
	if err != nil {
		return unknown
	}
	body, err := json.Marshal(request)
	if err != nil || len(body) > 1<<20 {
		return unknown
	}
	var checked contract.PluginSendRequestV1
	if DecodeNotificationBody(body, "PluginSendRequest", &checked) != nil {
		return unknown
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return unknown
	}
	httpRequest.GetBody = nil
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if setPluginBearer(httpRequest, runtime.AuthSecret) != nil {
		return unknown
	}
	client := c.client(endpoint)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		return unknown
	}
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	defer transport.CloseIdleConnections()
	response, err := client.Do(httpRequest)
	if err != nil {
		return unknown
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !contentTypeJSON(response.Header.Get("Content-Type")) {
		return unknown
	}
	raw, err := readBounded(response.Body, c.options.MaxResponseBytes)
	if err != nil {
		return unknown
	}
	var result contract.PluginSendResultV1
	if DecodeNotificationBody(raw, "PluginSendResult", &result) != nil || result.DeliveryID != request.DeliveryID {
		return unknown
	}
	if result.Error != nil {
		// Provider messages are untrusted and may echo credentials or recipients.
		result.Error.Code = safeProviderCode(result.Error.Code)
		result.Error.Message = "Plugin reported a notification submission outcome."
	}
	return result
}

var _ PluginSender = (*InstancePluginSender)(nil)
