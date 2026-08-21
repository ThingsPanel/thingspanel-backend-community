package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"project/internal/model"

	"github.com/go-basic/uuid"
)

const (
	marketTemplateImageMaxSize = 5 << 20
	marketTemplateAssetPrefix  = "/api/market/templates/assets/"
)

var marketTemplateImageDir = filepath.Join(".", "files", "deviceConfig")

// localizeMarketTemplateImage copies the resource-center-owned cover to local
// storage. Legacy device_config.image_url values are intentionally not fetched:
// they may point to an arbitrary publisher-controlled host.
func localizeMarketTemplateImage(ctx context.Context, client *MarketClient, fullData *model.MarketTemplateFullData) (*string, string, error) {
	if fullData == nil || strings.TrimSpace(fullData.CoverURL) == "" {
		return nil, "", nil
	}

	localURL, diskPath, err := downloadMarketTemplateImage(ctx, client, strings.TrimSpace(fullData.CoverURL), marketTemplateImageDir)
	if err != nil {
		return nil, "", err
	}
	return &localURL, diskPath, nil
}

func downloadMarketTemplateImage(ctx context.Context, client *MarketClient, coverURL, storageDir string) (string, string, error) {
	if client == nil || client.httpClient == nil {
		return "", "", fmt.Errorf("market client is not configured")
	}
	if err := validateMarketTemplateAssetURL(client.baseURL, coverURL); err != nil {
		return "", "", err
	}

	httpClient := *client.httpClient
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many market cover redirects")
		}
		return validateMarketTemplateAssetURL(client.baseURL, req.URL.String())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coverURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("create market cover request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download market cover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download market cover returned status %d", resp.StatusCode)
	}
	if resp.ContentLength > marketTemplateImageMaxSize {
		return "", "", fmt.Errorf("market cover exceeds %d bytes", marketTemplateImageMaxSize)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, marketTemplateImageMaxSize+1))
	if err != nil {
		return "", "", fmt.Errorf("read market cover: %w", err)
	}
	if len(data) == 0 {
		return "", "", fmt.Errorf("market cover is empty")
	}
	if len(data) > marketTemplateImageMaxSize {
		return "", "", fmt.Errorf("market cover exceeds %d bytes", marketTemplateImageMaxSize)
	}

	extension, err := marketTemplateImageExtension(http.DetectContentType(data))
	if err != nil {
		return "", "", err
	}

	dateDir := time.Now().Format("2006-01-02")
	finalDir := filepath.Join(storageDir, dateDir)
	stagingDir := filepath.Join(storageDir, ".staging")
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		return "", "", fmt.Errorf("create market cover directory: %w", err)
	}
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", "", fmt.Errorf("create market cover staging directory: %w", err)
	}

	fileName := uuid.New() + extension
	stagingPath := filepath.Join(stagingDir, fileName+".tmp")
	finalPath := filepath.Join(finalDir, fileName)
	if err := os.WriteFile(stagingPath, data, 0644); err != nil {
		return "", "", fmt.Errorf("write market cover staging file: %w", err)
	}
	defer os.Remove(stagingPath)

	if err := os.Rename(stagingPath, finalPath); err != nil {
		return "", "", fmt.Errorf("finalize market cover: %w", err)
	}

	localURL := "./" + path.Join("files", "deviceConfig", dateDir, fileName)
	return localURL, finalPath, nil
}

func validateMarketTemplateAssetURL(baseURL, assetURL string) error {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return fmt.Errorf("invalid market base URL")
	}
	asset, err := url.Parse(strings.TrimSpace(assetURL))
	if err != nil || asset.Scheme == "" || asset.Host == "" {
		return fmt.Errorf("invalid market cover URL")
	}
	if asset.User != nil || asset.Fragment != "" {
		return fmt.Errorf("market cover URL contains unsupported components")
	}
	if !sameMarketOrigin(base, asset) {
		return fmt.Errorf("market cover URL is not hosted by the configured resource center")
	}

	cleanPath := path.Clean(asset.Path)
	if cleanPath != asset.Path || !strings.HasPrefix(cleanPath, marketTemplateAssetPrefix) {
		return fmt.Errorf("market cover URL is outside the template asset path")
	}
	return nil
}

func sameMarketOrigin(base, asset *url.URL) bool {
	if !strings.EqualFold(base.Scheme, asset.Scheme) || normalizedURLPort(base) != normalizedURLPort(asset) {
		return false
	}
	if strings.EqualFold(base.Hostname(), asset.Hostname()) {
		return true
	}
	return isLoopbackHost(base.Hostname()) && isLoopbackHost(asset.Hostname())
}

func normalizedURLPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(value.Scheme, "http") {
		return "80"
	}
	return ""
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func marketTemplateImageExtension(contentType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", fmt.Errorf("unsupported market cover content type %q", contentType)
	}
}
