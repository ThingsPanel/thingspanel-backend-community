package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// downloadMetadataArtifact streams an authenticated native export into a new
// file. A hard-link in the same directory provides atomic no-overwrite
// publication; the private temporary file is removed on every error path.
func downloadMetadataArtifact(c *client, downloadID, target string) error {
	if downloadID == "" || strings.ContainsAny(downloadID, "/?#%") || downloadID == "." || downloadID == ".." {
		return fmt.Errorf("无效的下载 ID")
	}
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("需要输出文件路径")
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("目标文件已存在")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("无法检查目标文件: %w", err)
	}
	u, err := url.Parse(c.baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("无效的服务地址")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v2/metadata-downloads/" + downloadID
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("无法创建下载请求")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	// Redirects are not part of the export contract; never forward credentials
	// or silently validate content from a different endpoint.
	transport := *c.http
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := transport.Do(req)
	if err != nil {
		return fmt.Errorf("下载请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载服务返回 HTTP %d", resp.StatusCode)
	}
	tag := resp.Header.Get("ETag")
	if len(tag) != len(`"sha256-`)+64+1 || !strings.HasPrefix(tag, `"sha256-`) || !strings.HasSuffix(tag, `"`) {
		return fmt.Errorf("下载缺少有效的 SHA-256 ETag")
	}
	want, err := hex.DecodeString(tag[len(`"sha256-`) : len(tag)-1])
	if err != nil || len(want) != sha256.Size {
		return fmt.Errorf("下载的 SHA-256 ETag 无效")
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".tpctl-download-*")
	if err != nil {
		return fmt.Errorf("无法创建临时文件: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hash), resp.Body); err != nil {
		return fmt.Errorf("下载中断: %w", err)
	}
	if subtle.ConstantTimeCompare(want, hash.Sum(nil)) != 1 {
		return fmt.Errorf("下载 SHA-256 校验失败")
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("无法保存下载文件: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("无法关闭下载文件: %w", err)
	}
	if err := os.Link(name, target); err != nil {
		return fmt.Errorf("无法创建目标文件（可能已存在）: %w", err)
	}
	return nil
}
