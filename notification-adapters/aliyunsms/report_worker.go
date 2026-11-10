package aliyunsms

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encore.app/notification-adapters/sdk"
)

var (
	ErrUnmappedReport         = errors.New("SMS report has no exact accepted-send mapping")
	ErrUnrecognizedReportBody = errors.New("SMS report queue body format is not recognized")
)

type VisibilityQueue interface {
	CredentialedQueue
	MakeVisible(context.Context, string, int) error
}

type ScopedQueue interface {
	VisibilityQueue
	QueueNamespace() string
}

// ReceiptRoute is trusted operator configuration for one exact tenant and
// instance. No routing data is accepted from a provider report.
type ReceiptRoute struct {
	TenantID     string
	InstanceID   string
	CoreURL      string
	ReceiptToken string
}

type ReceiptRouteResolver interface {
	ResolveReceiptRoute(context.Context, SendLedgerRecord) (ReceiptRoute, error)
}

type StaticReceiptRoutes struct {
	routes map[string]ReceiptRoute
}

func NewStaticReceiptRoutes(routes []ReceiptRoute) (*StaticReceiptRoutes, error) {
	if len(routes) == 0 {
		return nil, errors.New("at least one operator receipt route is required")
	}
	result := &StaticReceiptRoutes{routes: make(map[string]ReceiptRoute, len(routes))}
	for _, route := range routes {
		if route.TenantID == "" || route.InstanceID == "" || route.ReceiptToken == "" {
			return nil, errors.New("operator receipt route is incomplete")
		}
		base, err := url.Parse(route.CoreURL)
		if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Opaque != "" {
			return nil, errors.New("operator receipt route must use a fixed HTTPS core URL")
		}
		key := receiptRouteKey(route.TenantID, route.InstanceID)
		if _, exists := result.routes[key]; exists {
			return nil, errors.New("duplicate operator receipt route scope")
		}
		result.routes[key] = route
	}
	return result, nil
}

func (r *StaticReceiptRoutes) ResolveReceiptRoute(_ context.Context, record SendLedgerRecord) (ReceiptRoute, error) {
	if r == nil || record.TenantID == "" || record.InstanceID == "" {
		return ReceiptRoute{}, errors.New("receipt route not found")
	}
	route, ok := r.routes[receiptRouteKey(record.TenantID, record.InstanceID)]
	if !ok {
		return ReceiptRoute{}, errors.New("receipt route not found")
	}
	return route, nil
}

func receiptRouteKey(tenantID, instanceID string) string {
	return ledgerRecordKey(tenantID, instanceID, "receipt-route")
}

type ReceiptPoster interface {
	PostReceipt(context.Context, ReceiptRoute, sdk.Receipt) error
}

type ReportWorker struct {
	queue         ScopedQueue
	ledger        SendLedger
	queueScope    string
	routes        ReceiptRouteResolver
	poster        ReceiptPoster
	visibilitySec int
}

func NewReportWorker(queue ScopedQueue, ledger SendLedger, routes ReceiptRouteResolver, poster ReceiptPoster) (*ReportWorker, error) {
	if queue == nil || ledger == nil || routes == nil || poster == nil {
		return nil, errors.New("MNS queue, shared send ledger, route resolver, and receipt poster are required")
	}
	if queue.QueueNamespace() == "" {
		return nil, errors.New("MNS queue namespace is required")
	}
	return &ReportWorker{queue: queue, ledger: ledger, queueScope: queue.QueueNamespace(), routes: routes, poster: poster, visibilitySec: 60}, nil
}

func (w *ReportWorker) RunOnce(ctx context.Context) error {
	if w == nil || ctx == nil {
		return errors.New("report worker and context are required")
	}
	message, err := w.queue.Pull(ctx)
	if errors.Is(err, ErrNoQueueMessage) {
		return nil
	}
	if err != nil {
		return err
	}
	if message.MessageID == "" || message.AckHandle == "" {
		return errors.New("MNS message identity is incomplete")
	}
	if err := w.process(ctx, message); err != nil {
		// The message stays durable in the provider queue. Delay it to avoid a
		// hot loop while keeping it available for mapping/config repair.
		_ = w.queue.MakeVisible(ctx, message.AckHandle, w.visibilitySec)
		return err
	}
	if err := w.queue.Ack(ctx, message.AckHandle); err != nil {
		return errors.New("MNS report acknowledgment failed after durable receipt acceptance")
	}
	return nil
}

func (w *ReportWorker) process(ctx context.Context, message QueueMessage) error {
	reports, err := decodeReportBatch(message.Body)
	if err != nil {
		return ErrUnrecognizedReportBody
	}
	if _, err := NormalizeReports(message.Body, w.queueScope); err != nil {
		return ErrUnrecognizedReportBody
	}
	for _, report := range reports {
		record, found, err := w.ledger.LookupAccepted(w.queueScope, report.BizID, report.PhoneNumber)
		if err != nil {
			return errors.New("could not resolve SMS report correlation")
		}
		if !found {
			return ErrUnmappedReport
		}
		route, err := w.routes.ResolveReceiptRoute(ctx, record)
		if err != nil || route.TenantID != record.TenantID || route.InstanceID != record.InstanceID || route.CoreURL == "" || route.ReceiptToken == "" {
			return errors.New("could not resolve trusted receipt route")
		}
		receipt, err := normalizeReport(report, w.queueScope)
		if err != nil {
			return ErrUnrecognizedReportBody
		}
		receipt.DeliveryID = record.DeliveryID
		if err := w.poster.PostReceipt(ctx, route, receipt); err != nil {
			return errors.New("notification core did not durably accept SMS receipt")
		}
	}
	return nil
}

func decodeReportBatch(raw []byte) ([]SmsReport, error) {
	if len(raw) == 0 || len(raw) > maxReportBytes {
		return nil, errors.New("invalid report batch")
	}
	return decodeSmsReports(raw)
}

// CoreReceiptPoster sends one standard receipt to the fixed HTTPS platform
// route for the resolved instance. It accepts only HTTP 200 as durable ACK.
type CoreReceiptPoster struct {
	client *http.Client
}

func NewCoreReceiptPoster() *CoreReceiptPoster {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          8,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &CoreReceiptPoster{client: &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (p *CoreReceiptPoster) PostReceipt(ctx context.Context, route ReceiptRoute, receipt sdk.Receipt) error {
	if p == nil || p.client == nil || ctx == nil || route.TenantID == "" || route.InstanceID == "" || route.ReceiptToken == "" || receipt.EventID == "" || receipt.ProviderMessageID == "" || receipt.DeliveryID == "" {
		return errors.New("invalid notification receipt request")
	}
	base, err := url.Parse(route.CoreURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Opaque != "" {
		return errors.New("invalid fixed notification core URL")
	}
	basePath := strings.TrimRight(base.EscapedPath(), "/")
	base.Path = strings.TrimRight(base.Path, "/") + "/api/v1/notification-instances/" + route.InstanceID + "/receipts"
	base.RawPath = basePath + "/api/v1/notification-instances/" + url.PathEscape(route.InstanceID) + "/receipts"
	body, err := json.Marshal(receipt)
	if err != nil {
		return errors.New("could not encode notification receipt")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return errors.New("could not create notification receipt request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+route.ReceiptToken)
	response, err := p.client.Do(request)
	if err != nil {
		return errors.New("notification receipt request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("notification core did not durably accept receipt")
	}
	ackBody, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(ackBody) > 64<<10 {
		return errors.New("notification receipt acknowledgment was incomplete")
	}
	return nil
}

var _ ReceiptPoster = (*CoreReceiptPoster)(nil)
