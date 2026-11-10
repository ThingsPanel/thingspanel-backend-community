// Package smtp provides a portable, single-recipient SMTP adapter.
package smtp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	netsmtp "net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"encore.app/notification-adapters/sdk"
)

const (
	pluginID         = "thingspanel.smtp"
	operationTimeout = 15 * time.Second
	maxMessageBytes  = 1 << 20
)

type Provider struct {
	servers map[string][]net.IPAddr
	cidrs   []net.IPNet
	roots   *x509.CertPool
	fixture bool
}

// New permits only exact host:port targets named by the operator. DNS is
// resolved once and pinned. Private service addresses are accepted only when
// they also fall in an operator-supplied network range; metadata/link-local
// addresses remain prohibited.
func New(allowedServers, allowedCIDRs []string) (*Provider, error) {
	if len(allowedServers) == 0 {
		return nil, errors.New("SMTP server allowlist is empty")
	}
	var cidrs []net.IPNet
	for _, raw := range allowedCIDRs {
		p, err := parsePrefix(raw)
		if err != nil {
			return nil, errors.New("invalid SMTP network allowlist")
		}
		cidrs = append(cidrs, p)
	}
	servers := make(map[string][]net.IPAddr, len(allowedServers))
	for _, raw := range allowedServers {
		host, port, err := net.SplitHostPort(raw)
		if err != nil || host == "" {
			return nil, errors.New("invalid SMTP server allowlist")
		}
		parsedPort, err := strconv.Atoi(port)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return nil, errors.New("invalid SMTP server allowlist")
		}
		lookupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		ips, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
		cancel()
		if err != nil || len(ips) == 0 {
			return nil, errors.New("SMTP server DNS resolution failed")
		}
		for _, ip := range ips {
			if prohibited(ip.IP) || !publicSMTPIP(ip.IP) && !covered(ip.IP, cidrs) {
				return nil, errors.New("SMTP server resolved outside approved addresses")
			}
		}
		servers[raw] = append([]net.IPAddr(nil), ips...)
	}
	return &Provider{servers: servers, cidrs: cidrs}, nil
}

// newFixtureProvider is package-test-only and allows independent loopback SMTP fixtures.
func newFixtureProvider(server string, roots *x509.CertPool) *Provider {
	return &Provider{servers: map[string][]net.IPAddr{server: {{IP: net.ParseIP("127.0.0.1")}}}, roots: roots, fixture: true}
}

func (p *Provider) Manifest() sdk.Manifest {
	return sdk.Manifest{APIVersion: sdk.APIVersion, PluginID: pluginID, Name: "SMTP email", PluginVersion: "1.0.1", Channels: []string{"email"}, ContentModes: []string{"text"},
		ConfigSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"host", "port", "from_email", "from_password"}, "properties": map[string]any{
			"host": map[string]any{"type": "string", "minLength": 1}, "port": map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}, "from_email": map[string]any{"type": "string", "format": "email"}, "from_password": map[string]any{"type": "string", "minLength": 1}, "ssl": map[string]any{"type": "boolean", "description": "true uses implicit TLS; false or omitted requires STARTTLS."},
		}},
		RecipientSchema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"kind", "address"}, "properties": map[string]any{"kind": map[string]any{"const": "email"}, "address": map[string]any{"type": "string", "format": "email"}}},
		SecretFields:    []string{"from_password"}, IdentityFields: []string{"host", "port", "from_email", "ssl"}, Capabilities: sdk.Capabilities{DeliveryReceipts: false}}
}

func (p *Provider) Validate(_ context.Context, req sdk.ValidateRequest) sdk.ValidateResult {
	var errs []sdk.FieldError
	if !keys(req.Config, "host", "port", "from_email", "from_password", "ssl") {
		errs = append(errs, fe("config", "unknown_field", "Configuration contains unsupported fields."))
	}
	host, ok := req.Config["host"].(string)
	port, portOK := integer(req.Config["port"])
	from, fromOK := req.Config["from_email"].(string)
	password, passOK := req.Config["from_password"].(string)
	if !ok || host == "" || !portOK || port < 1 || port > 65535 || !p.allowed(host, port) {
		errs = append(errs, fe("host", "server_not_allowed", "The SMTP host and port must match the operator allowlist."))
	}
	if !fromOK || mailbox(from) == "" {
		errs = append(errs, fe("from_email", "invalid_email", "A valid sender mailbox is required."))
	}
	if !passOK || password == "" {
		errs = append(errs, fe("from_password", "required", "An SMTP password is required."))
	}
	if raw, exists := req.Config["ssl"]; exists {
		if _, ok := raw.(bool); !ok {
			errs = append(errs, fe("ssl", "invalid", "The ssl value must be boolean."))
		}
	}
	return sdk.ValidateResult{Valid: len(errs) == 0, Errors: errs}
}
func (p *Provider) Health(context.Context) error {
	if len(p.servers) == 0 {
		return errors.New("SMTP server allowlist is empty")
	}
	return nil
}

func (p *Provider) Send(ctx context.Context, req sdk.SendRequest) (sdk.SendResult, error) {
	if req.Channel != "email" || req.Recipient.Kind != "email" || req.Content.Kind != "text" {
		return reject(req.DeliveryID, "unsupported_request", "SMTP accepts one text email target."), nil
	}
	if !keys(req.Config, "host", "port", "from_email", "from_password", "ssl") {
		return reject(req.DeliveryID, "invalid_configuration", "The SMTP configuration is invalid."), nil
	}
	host, ok := req.Config["host"].(string)
	port, portOK := integer(req.Config["port"])
	from, fromOK := req.Config["from_email"].(string)
	password, passOK := req.Config["from_password"].(string)
	if !ok || !portOK || !fromOK || !passOK || !p.allowed(host, port) || mailbox(from) == "" || password == "" {
		return reject(req.DeliveryID, "invalid_configuration", "The SMTP configuration is invalid."), nil
	}
	if len(req.Content.Title)+len(req.Content.Text) > maxMessageBytes {
		return reject(req.DeliveryID, "message_too_large", "The email message exceeds the size limit."), nil
	}
	to := mailbox(req.Recipient.Address)
	if to == "" {
		return reject(req.DeliveryID, "invalid_recipient", "The recipient email address is invalid."), nil
	}
	ssl := false
	if value, exists := req.Config["ssl"]; exists {
		ssl, ok = value.(bool)
		if !ok {
			return reject(req.DeliveryID, "invalid_configuration", "The SMTP configuration is invalid."), nil
		}
	}
	message := encodeMessage(from, to, req.Content.Title, req.Content.Text)
	server := net.JoinHostPort(host, strconv.Itoa(port))
	result := p.sendOnce(ctx, server, host, from, password, ssl, to, message, req.DeliveryID)
	return result, nil
}

func (p *Provider) sendOnce(ctx context.Context, server, host, from, password string, implicitTLS bool, recipient string, message []byte, id string) sdk.SendResult {
	ips, ok := p.servers[server]
	if !ok {
		return reject(id, "server_not_allowed", "The SMTP server is not allowed.")
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 0}
	var conn net.Conn
	var err error
	for _, ip := range ips {
		conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.IP.String(), portOf(server)))
		if err == nil {
			break
		}
	}
	if err != nil {
		return uncertain(id)
	}
	defer conn.Close()
	deadline := time.Now().Add(operationTimeout)
	if d, yes := ctx.Deadline(); yes && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if implicitTLS {
		conn = tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: p.roots})
		if err = conn.(*tls.Conn).HandshakeContext(ctx); err != nil {
			return uncertain(id)
		}
	}
	client, err := netsmtp.NewClient(conn, host)
	if err != nil {
		return uncertain(id)
	}
	defer client.Close()
	if err = client.Hello("notification-adapter"); err != nil {
		return uncertain(id)
	}
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return reject(id, "starttls_required", "The SMTP server does not advertise required STARTTLS.")
		}
		if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: p.roots}); err != nil {
			if _, yes := explicitSMTP(err); yes {
				return reject(id, "smtp_protocol_rejected", "The SMTP server rejected the TLS upgrade.")
			}
			return uncertain(id)
		}
	}
	if err = client.Auth(netsmtp.PlainAuth("", from, password, host)); err != nil {
		if _, yes := explicitSMTP(err); yes {
			return reject(id, "smtp_auth_rejected", "The SMTP server rejected authentication.")
		}
		return uncertain(id)
	}
	fromBox := mailbox(from)
	if err = client.Mail(fromBox); err != nil {
		if code, yes := explicitSMTP(err); yes {
			return smtpRejected(id, code)
		}
		return uncertain(id)
	}
	if err = client.Rcpt(recipient); err != nil {
		if code, yes := explicitSMTP(err); yes {
			return smtpRejected(id, code)
		}
		return uncertain(id)
	}
	w, err := client.Data()
	if err != nil {
		if code, yes := explicitSMTP(err); yes {
			return smtpRejected(id, code)
		}
		return uncertain(id)
	}
	// Once 354 has been received, any write or final response loss may follow a
	// completed provider side effect. Always return unknown and never retry.
	if _, err = w.Write(message); err != nil {
		_ = w.Close()
		return uncertain(id)
	}
	if err = w.Close(); err != nil {
		return uncertain(id)
	}
	return sdk.SendResult{DeliveryID: id, Outcome: "accepted", ReceiptExpected: false}
}

func encodeMessage(from, to, subject, body string) []byte {
	encodedSubject := mime.QEncoding.Encode("UTF-8", subject)
	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))
	var folded strings.Builder
	for start := 0; start < len(encodedBody); start += 76 {
		end := start + 76
		if end > len(encodedBody) {
			end = len(encodedBody)
		}
		folded.WriteString(encodedBody[start:end])
		folded.WriteString("\r\n")
	}
	return []byte("From: <" + mailbox(from) + ">\r\nTo: <" + to + ">\r\nSubject: " + encodedSubject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + folded.String())
}
func mailbox(raw string) string {
	if strings.ContainsAny(raw, "\r\n") {
		return ""
	}
	a, err := mail.ParseAddress(raw)
	if err != nil || a.Address == "" || strings.ContainsAny(a.Address, "\r\n<> ") {
		return ""
	}
	return a.Address
}
func integer(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		i := int(n)
		return i, n == float64(i)
	default:
		return 0, false
	}
}
func keys(m map[string]any, allowed ...string) bool {
	set := map[string]bool{}
	for _, x := range allowed {
		set[x] = true
	}
	for x := range m {
		if !set[x] {
			return false
		}
	}
	return true
}
func (p *Provider) allowed(host string, port int) bool {
	_, ok := p.servers[net.JoinHostPort(host, strconv.Itoa(port))]
	return ok
}
func portOf(server string) string { _, p, _ := net.SplitHostPort(server); return p }
func explicitSMTP(err error) (int, bool) {
	var e *textproto.Error
	if errors.As(err, &e) {
		return e.Code, true
	}
	return 0, false
}
func smtpRejected(id string, code int) sdk.SendResult {
	retry := code >= 400 && code < 500
	return sdk.SendResult{DeliveryID: id, Outcome: "rejected", Error: &sdk.DeliveryError{Code: "smtp_rejected", Message: "The SMTP server explicitly rejected the message before accepting DATA.", Retryable: retry}}
}
func reject(id, code, message string) sdk.SendResult {
	return sdk.SendResult{DeliveryID: id, Outcome: "rejected", Error: &sdk.DeliveryError{Code: code, Message: message, Retryable: false}}
}
func uncertain(id string) sdk.SendResult {
	return sdk.SendResult{DeliveryID: id, Outcome: "unknown", Error: &sdk.DeliveryError{Code: "submission_uncertain", Message: "Submission outcome is unknown; automatic retry is disabled.", Retryable: false}}
}
func fe(field, code, message string) sdk.FieldError {
	return sdk.FieldError{Field: field, Code: code, Message: message}
}

// parsePrefix and address helpers are separate so tests can verify operator
// ranges without depending on a DNS or external SMTP service.
func parsePrefix(raw string) (net.IPNet, error) { return parseIPPrefix(raw) }
func covered(ip net.IP, ranges []net.IPNet) bool {
	for _, r := range ranges {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}
func prohibited(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	if ip.To4() != nil {
		for _, cidr := range []string{"169.254.0.0/16", "100.100.100.200/32", "100.100.100.0/24"} {
			_, n, _ := net.ParseCIDR(cidr)
			if n.Contains(ip) {
				return true
			}
		}
	}
	return false
}
func publicSMTPIP(ip net.IP) bool { return ip.IsGlobalUnicast() && !ip.IsPrivate() && !prohibited(ip) }
func parseIPPrefix(raw string) (net.IPNet, error) {
	_, n, err := net.ParseCIDR(raw)
	if err != nil {
		return net.IPNet{}, fmt.Errorf("invalid prefix")
	}
	return *n, nil
}
