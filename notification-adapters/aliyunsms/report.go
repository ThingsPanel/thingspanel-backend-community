package aliyunsms

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"encore.app/notification-adapters/sdk"
)

const maxReportBytes = 1 << 20

var safeProviderCode = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// SmsReport is the domestic SMS report payload documented by Alibaba Cloud.
// Phone numbers and descriptions are intentionally not copied into normalized
// receipt messages.
type SmsReport struct {
	SendTime     string `json:"send_time"`
	ReportTime   string `json:"report_time"`
	Success      *bool  `json:"success"`
	ErrorCode    string `json:"err_code"`
	ErrorMessage string `json:"err_msg"`
	PhoneNumber  string `json:"phone_number"`
	SmsSize      string `json:"sms_size"`
	BizID        string `json:"biz_id"`
	OutID        string `json:"out_id"`
}

// CredentialedQueue is the boundary for an official authenticated queue
// consumer. A production implementation must verify the provider's queue
// signature before returning a message and acknowledge only after the sink
// durably accepts every normalized receipt.
type CredentialedQueue interface {
	Pull(context.Context) (QueueMessage, error)
	Ack(context.Context, string) error
}

type QueueMessage struct {
	MessageID string
	AckHandle string
	Body      []byte
}

type ReceiptSink interface {
	Accept(context.Context, sdk.Receipt) error
}

// ConsumeOne processes one already-authenticated queue message. It does not
// expose a network listener or implement provider queue authentication.
func ConsumeOne(ctx context.Context, queue CredentialedQueue, sink ReceiptSink, instanceNamespace string) error {
	if queue == nil || sink == nil {
		return errors.New("credentialed queue and receipt sink are required")
	}
	message, err := queue.Pull(ctx)
	if err != nil {
		return errors.New("could not pull a trusted report")
	}
	if message.MessageID == "" || message.AckHandle == "" {
		return errors.New("queue message has no message identity or acknowledgment handle")
	}
	receipts, err := NormalizeReports(message.Body, instanceNamespace)
	if err != nil {
		return err
	}
	for _, receipt := range receipts {
		if err := sink.Accept(ctx, receipt); err != nil {
			return errors.New("receipt sink did not durably accept report")
		}
	}
	if err := queue.Ack(ctx, message.AckHandle); err != nil {
		return errors.New("trusted report acknowledgment failed")
	}
	return nil
}

// NormalizeReports maps trusted, authenticated queue reports to the frozen
// plugin receipt union. Callers must obtain input through a credentialed queue
// adapter and send the returned receipts to the core's authenticated receipt
// API; this parser is not a public webhook handler.
func NormalizeReports(raw []byte, instanceNamespace string) ([]sdk.Receipt, error) {
	if len(raw) == 0 || len(raw) > maxReportBytes || strings.TrimSpace(instanceNamespace) == "" {
		return nil, errors.New("invalid SMS report batch")
	}
	reports, err := decodeSmsReports(raw)
	if err != nil {
		return nil, errors.New("invalid SMS report batch")
	}
	result := make([]sdk.Receipt, 0, len(reports))
	for _, report := range reports {
		receipt, err := normalizeReport(report, instanceNamespace)
		if err != nil {
			return nil, errors.New("invalid SMS report")
		}
		result = append(result, receipt)
	}
	return result, nil
}

func decodeSmsReports(raw []byte) ([]SmsReport, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	value, err := parseReportJSON(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON value")
	}
	items, ok := value.([]any)
	if !ok || len(items) == 0 || len(items) > 100 {
		return nil, errors.New("invalid report array")
	}
	allowed := map[string]bool{"send_time": true, "report_time": true, "success": true, "err_code": true, "err_msg": true, "phone_number": true, "sms_size": true, "biz_id": true, "out_id": true}
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("invalid report object")
		}
		for key := range fields {
			if !allowed[key] {
				return nil, errors.New("unsupported report field")
			}
		}
	}
	canonical, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	strict := json.NewDecoder(bytes.NewReader(canonical))
	strict.DisallowUnknownFields()
	var reports []SmsReport
	if err := strict.Decode(&reports); err != nil {
		return nil, err
	}
	var extra any
	if err := strict.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing report data")
	}
	return reports, nil
}

func parseReportJSON(dec *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, errors.New("report JSON is too deeply nested")
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		if tok == nil {
			return nil, errors.New("null report value")
		}
		return tok, nil
	}
	switch delim {
	case '{':
		object := make(map[string]any)
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok || strings.ContainsRune(key, '\x00') {
				return nil, errors.New("invalid report key")
			}
			if _, exists := object[key]; exists {
				return nil, errors.New("duplicate report key")
			}
			value, err := parseReportJSON(dec, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return nil, errors.New("invalid report object")
		}
		return object, nil
	case '[':
		var array []any
		for dec.More() {
			value, err := parseReportJSON(dec, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errors.New("invalid report array")
		}
		return array, nil
	default:
		return nil, errors.New("invalid report JSON")
	}
}

func normalizeReport(report SmsReport, namespace string) (sdk.Receipt, error) {
	if report.BizID == "" || len(report.BizID) > 128 || validatePhone(report.PhoneNumber) != nil || report.Success == nil {
		return sdk.Receipt{}, errors.New("missing correlation fields")
	}
	var status string
	if *report.Success {
		status = "delivered"
	} else {
		status = "failed"
	}
	occurredAt, err := parseReportTime(report.ReportTime)
	if err != nil {
		return sdk.Receipt{}, errors.New("missing report time")
	}
	providerCode := ""
	if report.ErrorCode != "" {
		if safeProviderCode.MatchString(report.ErrorCode) {
			providerCode = report.ErrorCode
		} else {
			providerCode = "provider_error"
		}
	}
	// The stable event key hashes provider correlation, status, time and target.
	// This distinguishes status transitions without exposing the phone number.
	key := strings.Join([]string{namespace, report.BizID, status, occurredAt.UTC().Format(time.RFC3339Nano), providerCode, report.PhoneNumber}, "\x00")
	digest := sha256.Sum256([]byte(key))
	return sdk.Receipt{
		EventID: hex.EncodeToString(digest[:]), ProviderMessageID: report.BizID,
		Status: status, OccurredAt: occurredAt.UTC(), ProviderCode: providerCode,
		Message: "Alibaba Cloud SMS delivery report.",
	}, nil
}

func parseReportTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.FixedZone("Asia/Shanghai", 8*60*60)); err == nil {
		return parsed, nil
	}
	return time.Time{}, errors.New("invalid time")
}
