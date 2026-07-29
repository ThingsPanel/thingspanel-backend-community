package api

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

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
