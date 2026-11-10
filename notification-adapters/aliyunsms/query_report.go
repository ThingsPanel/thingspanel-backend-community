package aliyunsms

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"encore.app/notification-adapters/sdk"
)

const (
	queryDetailsPageSize = 50
	queryDetailsMaxPages = 2
	queryDetailsMaxBody  = 1 << 20
)

// QueryCredentials contains the access key used only for authenticated,
// read-only QuerySendDetails calls. Callers should resolve it from the same
// protected instance snapshot used for the accepted send.
type QueryCredentials struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
}

// QueryRequest identifies one previously accepted SMS. SendDate uses the
// yyyyMMdd format required by Alibaba Cloud. BizID is the BizId returned by
// SendSms; PhoneNumber and InstanceNamespace are checked locally before a
// receipt is emitted.
type QueryRequest struct {
	PhoneNumber       string
	BizID             string
	SendDate          string
	InstanceNamespace string
}

// QueryResult is either still waiting for a provider report or contains one
// normalized terminal receipt. Pending is true when Alibaba has not indexed
// the record yet, reports SendStatus=1, or has not returned a terminal carrier
// code.
type QueryResult struct {
	Pending bool
	Receipt *sdk.Receipt
}

type querySendDetailsResponse struct {
	Code              string          `json:"Code"`
	TotalCount        json.RawMessage `json:"TotalCount"`
	SmsSendDetailDTOs struct {
		SmsSendDetailDTO []querySendDetail `json:"SmsSendDetailDTO"`
	} `json:"SmsSendDetailDTOs"`
}

type querySendDetail struct {
	ErrCode     string `json:"ErrCode"`
	ReceiveDate string `json:"ReceiveDate"`
	SendDate    string `json:"SendDate"`
	PhoneNum    string `json:"PhoneNum"`
	SendStatus  int    `json:"SendStatus"`
}

// PollAccepted queries one previously accepted BizId. It only calls the
// QuerySendDetails action and never calls SendSms. Provider details and
// credentials are intentionally omitted from all returned errors.
func (p *Provider) PollAccepted(ctx context.Context, credentials QueryCredentials, query QueryRequest) (QueryResult, error) {
	if p == nil || p.client == nil || ctx == nil || credentials.AccessKeyID == "" || credentials.AccessKeySecret == "" || query.BizID == "" || len(query.BizID) > 128 || validatePhone(query.PhoneNumber) != nil || strings.TrimSpace(query.InstanceNamespace) == "" {
		return QueryResult{}, errors.New("invalid Alibaba Cloud receipt query")
	}
	if _, err := time.Parse("20060102", query.SendDate); err != nil || len(query.SendDate) != 8 {
		return QueryResult{}, errors.New("invalid Alibaba Cloud receipt query")
	}
	if credentials.SecurityToken != "" && len(credentials.SecurityToken) > 4096 {
		return QueryResult{}, errors.New("invalid Alibaba Cloud receipt query")
	}
	endpoint, err := url.Parse(p.url)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.Host != p.host || endpoint.Path != "/" || endpoint.User != nil || endpoint.Fragment != "" {
		return QueryResult{}, errors.New("invalid Alibaba Cloud receipt endpoint")
	}

	var matches []querySendDetail
	for page := 1; page <= queryDetailsMaxPages; page++ {
		response, err := p.querySendDetailsPage(ctx, endpoint, credentials, query, page)
		if err != nil {
			return QueryResult{}, err
		}
		count, err := parseQueryTotalCount(response.TotalCount)
		if err != nil || count > queryDetailsPageSize*queryDetailsMaxPages {
			return QueryResult{}, errors.New("Alibaba Cloud receipt query exceeded its page bound")
		}
		for _, detail := range response.SmsSendDetailDTOs.SmsSendDetailDTO {
			if detail.PhoneNum == query.PhoneNumber && queryDetailSendDate(detail.SendDate) == query.SendDate {
				matches = append(matches, detail)
			}
		}
		if count <= page*queryDetailsPageSize {
			break
		}
		if len(response.SmsSendDetailDTOs.SmsSendDetailDTO) == 0 {
			return QueryResult{}, errors.New("Alibaba Cloud receipt query returned an incomplete page")
		}
		if page == queryDetailsMaxPages {
			return QueryResult{}, errors.New("Alibaba Cloud receipt query exceeded its page bound")
		}
	}
	if len(matches) > 1 {
		return QueryResult{}, errors.New("Alibaba Cloud receipt query returned an ambiguous match")
	}
	if len(matches) == 0 {
		return QueryResult{Pending: true}, nil
	}
	return normalizeQueryDetail(matches[0], query)
}

func (p *Provider) querySendDetailsPage(ctx context.Context, endpoint *url.URL, credentials QueryCredentials, query QueryRequest, page int) (querySendDetailsResponse, error) {
	params := url.Values{
		"BizId":       {query.BizID},
		"PhoneNumber": {query.PhoneNumber},
		"SendDate":    {query.SendDate},
		"PageSize":    {strconv.Itoa(queryDetailsPageSize)},
		"CurrentPage": {strconv.Itoa(page)},
	}
	canonicalQuery := canonicalQueryString(params)
	requestURL := *endpoint
	requestURL.RawQuery = canonicalQuery
	nonce, err := p.newID()
	if err != nil || nonce == "" {
		return querySendDetailsResponse{}, errors.New("Alibaba Cloud receipt query failed")
	}
	date := p.now().UTC().Format("2006-01-02T15:04:05Z")
	contentHash := sha256Hex(nil)
	headers := map[string]string{
		"host":                  p.host,
		"x-acs-action":          "QuerySendDetails",
		"x-acs-content-sha256":  contentHash,
		"x-acs-date":            date,
		"x-acs-signature-nonce": nonce,
		"x-acs-version":         apiVersion,
	}
	if credentials.SecurityToken != "" {
		headers["x-acs-security-token"] = credentials.SecurityToken
	}
	authorization := signV3(http.MethodPost, "/", canonicalQuery, headers, contentHash, credentials.AccessKeyID, credentials.AccessKeySecret)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), bytes.NewReader(nil))
	if err != nil {
		return querySendDetailsResponse{}, errors.New("Alibaba Cloud receipt query failed")
	}
	for key, value := range headers {
		if key != "host" {
			request.Header.Set(key, value)
		}
	}
	request.Host = p.host
	request.Header.Set("Authorization", authorization)
	response, err := p.client.Do(request)
	if err != nil {
		return querySendDetailsResponse{}, errors.New("Alibaba Cloud receipt query failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, queryDetailsMaxBody+1))
	if err != nil || len(body) > queryDetailsMaxBody || response.StatusCode != http.StatusOK {
		return querySendDetailsResponse{}, errors.New("Alibaba Cloud receipt query failed")
	}
	var result querySendDetailsResponse
	if json.Unmarshal(body, &result) != nil || result.Code != "OK" {
		return querySendDetailsResponse{}, errors.New("Alibaba Cloud receipt query failed")
	}
	return result, nil
}

func parseQueryTotalCount(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return 0, errors.New("missing total count")
	}
	var text string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, err
		}
	} else {
		text = string(raw)
	}
	count, err := strconv.Atoi(text)
	if err != nil || count < 0 {
		return 0, errors.New("invalid total count")
	}
	return count, nil
}

func queryDetailSendDate(value string) string {
	parsed, err := parseAlibabaQueryTime(value)
	if err != nil {
		return ""
	}
	return parsed.In(alibabaChinaTime).Format("20060102")
}

func normalizeQueryDetail(detail querySendDetail, query QueryRequest) (QueryResult, error) {
	switch detail.SendStatus {
	case 1:
		return QueryResult{Pending: true}, nil
	case 2:
		return queryReceipt(detail, query, "2")
	case 3:
		if strings.EqualFold(detail.ErrCode, "DELIVERED") {
			return queryReceipt(detail, query, "1")
		}
		if detail.ErrCode == "" {
			return QueryResult{Pending: true}, nil
		}
		return queryReceipt(detail, query, "2")
	default:
		return QueryResult{}, errors.New("Alibaba Cloud receipt query returned an unsupported status")
	}
}

func queryReceipt(detail querySendDetail, query QueryRequest, reportStatus string) (QueryResult, error) {
	occurredAtText := detail.ReceiveDate
	if occurredAtText == "" {
		occurredAtText = detail.SendDate
	}
	occurredAt, err := parseAlibabaQueryTime(occurredAtText)
	if err != nil {
		return QueryResult{}, errors.New("Alibaba Cloud receipt query returned an invalid time")
	}
	providerCode := ""
	if detail.ErrCode != "" {
		if safeProviderCode.MatchString(detail.ErrCode) {
			providerCode = detail.ErrCode
		} else {
			providerCode = "provider_error"
		}
	}
	key := strings.Join([]string{query.InstanceNamespace, query.BizID, reportStatus, occurredAt.UTC().Format(time.RFC3339Nano), providerCode, query.PhoneNumber}, "\x00")
	digest := sha256.Sum256([]byte(key))
	status := "failed"
	if reportStatus == "1" {
		status = "delivered"
	}
	return QueryResult{Receipt: &sdk.Receipt{
		EventID: hex.EncodeToString(digest[:]), ProviderMessageID: query.BizID,
		Status: status, OccurredAt: occurredAt.UTC(), ProviderCode: providerCode,
		Message: "Alibaba Cloud SMS delivery report.",
	}}, nil
}

var alibabaChinaTime = time.FixedZone("Asia/Shanghai", 8*60*60)

func parseAlibabaQueryTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, errors.New("empty provider time")
	}
	for _, layout := range []string{time.RFC3339, time.RFC1123Z} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, value, alibabaChinaTime); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid provider time")
}
