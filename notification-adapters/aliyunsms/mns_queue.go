package aliyunsms

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	mnsVersion       = "2015-06-06"
	mnsMaxBody       = 1 << 20
	mnsDefaultWait   = 10
	mnsMaxWait       = 30
	mnsMaxVisibility = 900
)

var (
	ErrNoQueueMessage = errors.New("no visible SMS report message")
	queueNamePattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	mnsAccountPattern = regexp.MustCompile(`^[0-9]{1,32}$`)
)

type MNSConfig struct {
	AccountID       string
	Region          string
	QueueName       string
	AccessKeyID     string
	AccessKeySecret string
	WaitSeconds     int
}

type mnsDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// MNSQueue uses only the fixed Alibaba HTTPS endpoint for the configured
// account and region. It never follows redirects or reads proxy environment.
type MNSQueue struct {
	config   MNSConfig
	endpoint *url.URL
	client   mnsDoer
	now      func() time.Time
}

func (q *MNSQueue) QueueNamespace() string {
	return queueNamespace(q.config.AccessKeyID, q.config.Region)
}

func NewMNSQueue(config MNSConfig) (*MNSQueue, error) {
	return newMNSQueue(config, newMNSHTTPClient(), time.Now, false)
}

func newMNSHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   3 * time.Second,
			ResponseHeaderTimeout: 12 * time.Second,
			IdleConnTimeout:       30 * time.Second,
			MaxIdleConns:          2,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
		Timeout:       35 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func newMNSQueue(config MNSConfig, client mnsDoer, now func() time.Time, allowTestEndpoint bool) (*MNSQueue, error) {
	if client == nil || now == nil || !mnsAccountPattern.MatchString(config.AccountID) || strings.TrimSpace(config.AccessKeyID) == "" || strings.TrimSpace(config.AccessKeySecret) == "" || !validRegion(config.Region) || !queueNamePattern.MatchString(config.QueueName) {
		return nil, errors.New("invalid Alibaba MNS queue configuration")
	}
	if config.WaitSeconds == 0 {
		config.WaitSeconds = mnsDefaultWait
	}
	if config.WaitSeconds < 0 || config.WaitSeconds > mnsMaxWait {
		return nil, errors.New("invalid Alibaba MNS wait duration")
	}
	endpoint := &url.URL{Scheme: "https", Host: config.AccountID + ".mns." + config.Region + ".aliyuncs.com"}
	if allowTestEndpoint {
		endpoint = &url.URL{Scheme: "https", Host: config.AccountID}
	}
	return &MNSQueue{config: config, endpoint: endpoint, client: client, now: now}, nil
}

func (q *MNSQueue) Pull(ctx context.Context) (QueueMessage, error) {
	if ctx == nil {
		return QueueMessage{}, errors.New("MNS request context is required")
	}
	requestURL := *q.endpoint
	requestURL.Path = "/queues/" + q.config.QueueName + "/messages"
	requestURL.RawQuery = url.Values{"waitseconds": {fmt.Sprint(q.config.WaitSeconds)}}.Encode()
	response, err := q.do(ctx, http.MethodGet, &requestURL, nil)
	if err != nil {
		return QueueMessage{}, errors.New("could not receive SMS report from MNS")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		if readErr == nil && mnsMessageMissing(data) {
			return QueueMessage{}, ErrNoQueueMessage
		}
		return QueueMessage{}, errors.New("MNS queue receive request failed")
	}
	if response.StatusCode != http.StatusOK {
		return QueueMessage{}, errors.New("MNS rejected SMS report receive request")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, mnsMaxBody+1))
	if err != nil || len(data) == 0 || len(data) > mnsMaxBody {
		return QueueMessage{}, errors.New("MNS returned an invalid SMS report message")
	}
	var envelope struct {
		XMLName       xml.Name `xml:"Message"`
		MessageID     string   `xml:"MessageId"`
		ReceiptHandle string   `xml:"ReceiptHandle"`
		MessageBody   string   `xml:"MessageBody"`
	}
	if err := xml.Unmarshal(data, &envelope); err != nil || envelope.XMLName.Local != "Message" || envelope.MessageID == "" || envelope.ReceiptHandle == "" || envelope.MessageBody == "" {
		return QueueMessage{}, errors.New("MNS returned an invalid SMS report envelope")
	}
	return QueueMessage{MessageID: envelope.MessageID, AckHandle: envelope.ReceiptHandle, Body: []byte(envelope.MessageBody)}, nil
}

func mnsMessageMissing(data []byte) bool {
	var response struct {
		Code string `xml:"Code"`
	}
	return xml.Unmarshal(data, &response) == nil && response.Code == "MessageNotExist"
}

func (q *MNSQueue) Ack(ctx context.Context, ackHandle string) error {
	if ackHandle == "" {
		return errors.New("MNS acknowledgment handle is required")
	}
	requestURL := *q.endpoint
	requestURL.Path = "/queues/" + q.config.QueueName + "/messages"
	requestURL.RawQuery = url.Values{"ReceiptHandle": {ackHandle}}.Encode()
	response, err := q.do(ctx, http.MethodDelete, &requestURL, nil)
	if err != nil {
		return errors.New("could not acknowledge SMS report in MNS")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return errors.New("MNS rejected SMS report acknowledgment")
	}
	return nil
}

// MakeVisible delays an unmatched message without deleting it. A later worker
// pass can retry the exact ledger lookup after deployment/operator repair.
func (q *MNSQueue) MakeVisible(ctx context.Context, ackHandle string, delaySeconds int) error {
	if ackHandle == "" || delaySeconds < 1 || delaySeconds > mnsMaxVisibility {
		return errors.New("invalid MNS visibility request")
	}
	requestURL := *q.endpoint
	requestURL.Path = "/queues/" + q.config.QueueName + "/messages"
	requestURL.RawQuery = url.Values{"ReceiptHandle": {ackHandle}}.Encode()
	body := []byte(fmt.Sprintf("<VisibilityTimeout>%d</VisibilityTimeout>", delaySeconds))
	response, err := q.do(ctx, http.MethodPut, &requestURL, body)
	if err != nil {
		return errors.New("could not delay unmatched SMS report")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("MNS rejected unmatched SMS report visibility change")
	}
	return nil
}

func (q *MNSQueue) do(ctx context.Context, method string, requestURL *url.URL, body []byte) (*http.Response, error) {
	if requestURL.Scheme != "https" || requestURL.Host != q.endpoint.Host || requestURL.User != nil || requestURL.Fragment != "" || requestURL.Opaque != "" {
		return nil, errors.New("invalid fixed MNS endpoint")
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, errors.New("could not create MNS request")
	}
	date := q.now().UTC().Format(http.TimeFormat)
	request.Header.Set("Date", date)
	request.Header.Set("x-mns-version", mnsVersion)
	if len(body) > 0 {
		request.Header.Set("Content-Type", "text/xml;charset=utf-8")
	}
	request.Header.Set("Authorization", signMNSRequest(method, requestURL.RequestURI(), request.Header, q.config.AccessKeyID, q.config.AccessKeySecret))
	return q.client.Do(request)
}

func signMNSRequest(method, resource string, headers http.Header, accessKeyID, accessKeySecret string) string {
	contentMD5 := headers.Get("Content-MD5")
	contentType := headers.Get("Content-Type")
	date := headers.Get("Date")
	keys := make([]string, 0)
	for key := range headers {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-mns-") {
			keys = append(keys, lower)
		}
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for _, key := range keys {
		canonical.WriteString(key)
		canonical.WriteByte(':')
		canonical.WriteString(headers.Get(key))
		canonical.WriteByte('\n')
	}
	stringToSign := strings.ToUpper(method) + "\n" + contentMD5 + "\n" + contentType + "\n" + date + "\n" + canonical.String() + resource
	mac := hmac.New(sha1.New, []byte(accessKeySecret))
	_, _ = io.WriteString(mac, stringToSign)
	return "MNS " + accessKeyID + ":" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

var _ CredentialedQueue = (*MNSQueue)(nil)
var _ interface {
	MakeVisible(context.Context, string, int) error
} = (*MNSQueue)(nil)
