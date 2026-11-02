package unipush

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Directory struct {
	base, token, deployment, appID string
	client                         *http.Client
}
type device struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	UserID   string `json:"userId"`
	AppID    string `json:"appId"`
	CID      string `json:"cid"`
}

func validPart(s string) bool {
	return s != "" && len(s) <= 256 && !strings.ContainsAny(s, " ,\t\r\n\x00/\\?&#")
}

// ConfigureDirectory fixes the tenant member directory and the DCloud App at
// process startup. A tenant cannot supply an arbitrary callback or App ID.
func (p *Provider) ConfigureDirectory(base, token, deployment, appID, cloudToken string, allowHTTP bool) error {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(allowHTTP && u.Scheme == "http")) || len(token) < 32 || strings.ContainsAny(token, " \t\r\n") || !validPart(deployment) || !validPart(appID) || len(cloudToken) < 32 || strings.ContainsAny(cloudToken, " \t\r\n") {
		return errors.New("invalid App directory or cloud credential")
	}
	p.directory = &Directory{strings.TrimRight(base, "/"), token, deployment, appID, p.client}
	p.cloudToken = cloudToken
	return nil
}

func (d *Directory) resolve(ctx context.Context, tenant, id string) (device, error) {
	var out device
	unavailable := errors.New("device binding unavailable")
	if d == nil || !validPart(tenant) || !validPart(id) {
		return out, unavailable
	}
	req, err := http.NewRequestWithContext(ctx, "GET", d.base+"/api/v1/notification/source-app-devices/"+url.PathEscape(id)+"?tenantId="+url.QueryEscape(tenant), nil)
	if err != nil {
		return out, unavailable
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("X-Notification-Deployment", d.deployment)
	res, err := d.client.Do(req)
	if err != nil {
		return out, unavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return out, unavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8193))
	if err != nil || len(raw) > 8192 || json.Unmarshal(raw, &out) != nil || out.ID != id || out.TenantID != tenant || out.AppID != d.appID || !validPart(out.CID) || !validPart(out.UserID) {
		return device{}, unavailable
	}
	return out, nil
}
