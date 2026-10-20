package api

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// thingsVisPublicAPIBaseURL resolves the API from the backend container's
// network. The browser origin is only a fallback for deployments without an
// internal route to ThingsVis.
func thingsVisPublicAPIBaseURL(c *gin.Context) (string, error) {
	configured := strings.TrimSpace(os.Getenv("GOTP_THINGSVIS_PUBLIC_API_BASE_URL"))
	if configured == "" {
		configured = strings.TrimSpace(viper.GetString("thingsvis.public_api_base_url"))
	}
	if configured = strings.TrimRight(configured, "/"); configured != "" {
		parsed, err := url.Parse(configured)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("invalid thingsvis.public_api_base_url")
		}
		return configured, nil
	}
	origin, err := requestPublicOrigin(c)
	if err != nil {
		return "", err
	}
	return origin + "/thingsvis-api", nil
}

func requestPublicOrigin(c *gin.Context) (string, error) {
	requestHost := firstHeaderValue(c.GetHeader("X-Forwarded-Host"))
	if requestHost == "" {
		requestHost = c.Request.Host
	}

	for _, rawURL := range []string{c.GetHeader("Origin"), c.GetHeader("Referer")} {
		parsed, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			continue
		}
		if requestHost == "" || sameHostname(parsed.Host, requestHost) {
			return parsed.Scheme + "://" + parsed.Host, nil
		}
	}

	if requestHost == "" {
		return "", fmt.Errorf("request host is missing")
	}
	scheme := firstHeaderValue(c.GetHeader("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
	}
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported request scheme %s", scheme)
	}
	return scheme + "://" + requestHost, nil
}

func firstHeaderValue(value string) string {
	if index := strings.IndexByte(value, ','); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func sameHostname(left, right string) bool {
	return strings.EqualFold(hostname(left), hostname(right))
}

func hostname(host string) string {
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		return parsedHost
	}
	return strings.Trim(host, "[]")
}
