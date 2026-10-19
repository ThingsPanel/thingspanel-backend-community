// Package brokersemantic reads the device's pinned, published ThingModel from
// the existing Encore API. It stores no model state and performs GET requests
// only.
package brokersemantic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid             = errors.New("brokersemantic: invalid request or configuration")
	ErrUnavailable         = errors.New("brokersemantic: semantic source unavailable")
	ErrNotFound            = errors.New("brokersemantic: device or binding not found")
	ErrSemanticUnavailable = errors.New("brokersemantic: canonical published semantic unavailable")
	ErrResponse            = errors.New("brokersemantic: invalid semantic source response")
)

const (
	requestTimeout = 2 * time.Second
	maxResponse    = 1 << 20
	pageSize       = 200
	maxVersions    = 1000
)

type Config struct {
	BaseURL     string
	Development bool
}

type Client struct {
	base *url.URL
	http *http.Client
}

// Pins identify the immutable model snapshot and the resource revision
// published with it.
type Pins struct {
	TenantID          string `json:"tenant_id"`
	SubjectID         string `json:"subject_id"`
	DeviceID          string `json:"device_id"`
	TemplateID        string `json:"template_id"`
	ModelID           string `json:"model_id"`
	SnapshotID        string `json:"snapshot_id"`
	PublishedRevision uint64 `json:"published_revision"`
	ResourceRevision  uint64 `json:"resource_revision"`
}

type Description struct {
	Pins      Pins            `json:"pins"`
	Model     json.RawMessage `json:"model"`
	Resources json.RawMessage `json:"resources"`
}

// NewClient accepts only a configured HTTPS origin. Plain HTTP is allowed only
// for explicit development configuration on a loopback address.
func NewClient(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrInvalid
	}
	if u.Path != "" && u.Path != "/" {
		return nil, ErrInvalid
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		ip := net.ParseIP(host)
		if !cfg.Development || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, ErrInvalid
		}
	default:
		return nil, ErrInvalid
	}
	u.Path = ""
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: requestTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   requestTimeout,
		ResponseHeaderTimeout: requestTimeout,
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
	}
	return &Client{base: u, http: &http.Client{
		Transport:     transport,
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Describe resolves device -> template -> exact snapshot -> immutable version.
// TenantID and SubjectID must come from the server's authenticated request
// context; bearer must be a delegated credential already bound by that server
// to the same subject and tenant. This client does not inspect or verify the
// bearer claims, so its SubjectID pin records the trusted caller context and
// is not a claim independently confirmed by the remote API. These values are
// never accepted from model-generated arguments.
func (c *Client) Describe(ctx context.Context, tenantID, subjectID, bearer, deviceID string) (Description, error) {
	if c == nil || c.http == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(subjectID) == "" || strings.TrimSpace(bearer) == "" || strings.TrimSpace(deviceID) == "" {
		return Description{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var device struct {
		ID         string `json:"id"`
		TenantID   string `json:"tenant_id"`
		TemplateID string `json:"device_template_id"`
	}
	if err := c.getJSON(ctx, bearer, "/api/devices/"+url.PathEscape(deviceID), &device); err != nil {
		return Description{}, err
	}
	if device.ID != deviceID || device.TenantID != tenantID || device.TemplateID == "" {
		return Description{}, ErrSemanticUnavailable
	}
	var template struct {
		ID         string `json:"id"`
		TenantID   string `json:"tenant_id"`
		ModelID    string `json:"thing_model_id"`
		SnapshotID string `json:"thing_model_snapshot_id"`
	}
	if err := c.getJSON(ctx, bearer, "/api/device-templates/"+url.PathEscape(device.TemplateID), &template); err != nil {
		return Description{}, err
	}
	if template.ID != device.TemplateID || template.TenantID != tenantID || template.ModelID == "" || template.SnapshotID == "" {
		return Description{}, ErrSemanticUnavailable
	}
	version, err := c.findSnapshot(ctx, bearer, template.ModelID, template.SnapshotID)
	if err != nil {
		return Description{}, err
	}
	path := "/api/v2/thing-models/" + url.PathEscape(template.ModelID) + "/versions/" + strconv.FormatUint(version.PublishedRevision, 10)
	body, err := c.get(ctx, bearer, path)
	if err != nil {
		return Description{}, err
	}
	var detail struct {
		ModelID           string          `json:"modelID"`
		PublishedRevision uint64          `json:"publishedRevision"`
		SnapshotID        string          `json:"snapshotID"`
		Model             json.RawMessage `json:"model"`
		Resources         json.RawMessage `json:"resources"`
	}
	if err := decodeStrictJSON(body, &detail); err != nil || detail.ModelID != template.ModelID || detail.SnapshotID != template.SnapshotID || detail.PublishedRevision != version.PublishedRevision || !validJSONObject(detail.Model) || !validJSONObject(detail.Resources) {
		return Description{}, ErrSemanticUnavailable
	}
	var resources struct {
		ModelID  string  `json:"modelID"`
		Revision *uint64 `json:"revision"`
	}
	if err := decodeStrictJSON(detail.Resources, &resources); err != nil || resources.ModelID != template.ModelID || resources.Revision == nil {
		return Description{}, ErrSemanticUnavailable
	}
	return Description{
		Pins:  Pins{TenantID: tenantID, SubjectID: subjectID, DeviceID: device.ID, TemplateID: template.ID, ModelID: template.ModelID, SnapshotID: template.SnapshotID, PublishedRevision: detail.PublishedRevision, ResourceRevision: *resources.Revision},
		Model: append(json.RawMessage(nil), detail.Model...), Resources: append(json.RawMessage(nil), detail.Resources...),
	}, nil
}

type versionList struct {
	Items    []versionSummary `json:"items"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
	Total    int              `json:"total"`
}

type versionSummary struct {
	PublishedRevision uint64 `json:"publishedRevision"`
	SnapshotID        string `json:"snapshotID"`
}

func (c *Client) findSnapshot(ctx context.Context, bearer, modelID, snapshotID string) (versionSummary, error) {
	var match versionSummary
	matches := 0
	firstTotal := -1
	for page := 1; page <= maxVersions/pageSize+1; page++ {
		path := "/api/v2/thing-models/" + url.PathEscape(modelID) + "/versions?page=" + strconv.Itoa(page) + "&pageSize=" + strconv.Itoa(pageSize)
		body, err := c.get(ctx, bearer, path)
		if err != nil {
			return versionSummary{}, err
		}
		var result versionList
		if err := decodeStrictJSON(body, &result); err != nil || result.Page != page || result.PageSize != pageSize || result.Total < 0 || len(result.Items) > pageSize {
			return versionSummary{}, ErrResponse
		}
		if firstTotal < 0 {
			firstTotal = result.Total
			if firstTotal > maxVersions {
				return versionSummary{}, ErrSemanticUnavailable
			}
		} else if result.Total != firstTotal {
			return versionSummary{}, ErrResponse
		}
		remaining := firstTotal - (page-1)*pageSize
		if remaining < 0 {
			remaining = 0
		}
		expectedItems := pageSize
		if remaining < expectedItems {
			expectedItems = remaining
		}
		if len(result.Items) != expectedItems {
			return versionSummary{}, ErrResponse
		}
		for _, item := range result.Items {
			if item.PublishedRevision == 0 || item.SnapshotID == "" {
				return versionSummary{}, ErrResponse
			}
			if item.SnapshotID == snapshotID {
				match = item
				matches++
			}
		}
		if page*pageSize >= firstTotal {
			break
		}
	}
	if matches != 1 {
		return versionSummary{}, ErrSemanticUnavailable
	}
	return match, nil
}

func (c *Client) getJSON(ctx context.Context, bearer, path string, out any) error {
	body, err := c.get(ctx, bearer, path)
	if err != nil {
		return err
	}
	if err := decodeStrictJSON(body, out); err != nil {
		return ErrResponse
	}
	return nil
}

func (c *Client) get(ctx context.Context, bearer, path string) ([]byte, error) {
	u := *c.base
	ref, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(ref.Path, "/") || ref.IsAbs() || ref.Host != "" || ref.Fragment != "" {
		return nil, ErrInvalid
	}
	u.Path = ref.Path
	u.RawPath = ref.RawPath
	u.RawQuery = ref.RawQuery
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrInvalid
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, ErrUnavailable
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrUnavailable
	}
	reader := io.LimitReader(resp.Body, maxResponse+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(body) > maxResponse {
		return nil, ErrResponse
	}
	return body, nil
}

func validJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 1 && trimmed[0] == '{' && json.Valid(trimmed)
}

func decodeStrictJSON(data []byte, dst any) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON")
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON value")
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("JSON nesting too deep")
		}
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for d.More() {
				keyToken, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("invalid object key")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate object key")
				}
				seen[key] = struct{}{}
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		default:
			return fmt.Errorf("unexpected delimiter")
		}
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
