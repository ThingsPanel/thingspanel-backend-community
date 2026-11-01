package main

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const nativeAssetMaxBytes int64 = 10 * 1024 * 1024

// uploadNativeAsset uses the fileassets handler's exact two multipart fields.
// The caller must provide a durable idempotency key and retain it for retry.
func uploadNativeAsset(c *client, path, idem, requestID string) (*response, *apiError) {
	if strings.TrimSpace(path) == "" || !validNativeIdempotencyKey(idem) || !validNativeBaseURL(c.baseURL) {
		return nil, &apiError{Code: "usage", Message: "需要图片文件和有效的幂等键", ExitCode: exitUsage}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, &apiError{Code: "usage", Message: "无法打开图片文件", ExitCode: exitUsage}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > nativeAssetMaxBytes {
		return nil, &apiError{Code: "usage", Message: "图片须为不超过 10 MiB 的非空普通文件", ExitCode: exitUsage}
	}
	reader, writer := io.Pipe()
	mw := multipart.NewWriter(writer)
	contentType := mw.FormDataContentType()
	done := make(chan error, 1)
	go func() {
		err := mw.WriteField("purpose", "template-image")
		if err == nil {
			var part io.Writer
			part, err = mw.CreateFormFile("file", filepath.Base(path))
			if err == nil {
				var count int64
				count, err = io.Copy(part, io.LimitReader(f, nativeAssetMaxBytes+1))
				if err == nil && count > nativeAssetMaxBytes {
					err = errors.New("file exceeds limit")
				}
			}
		}
		if err == nil {
			err = mw.Close()
		}
		_ = writer.CloseWithError(err)
		done <- err
	}()
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/api/v2/file-assets", reader)
	if err != nil {
		_ = reader.Close()
		<-done
		return nil, &apiError{Code: "usage", Message: "无法创建上传请求", ExitCode: exitUsage}
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", idem)
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	transport := *c.http
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, callErr := transport.Do(req)
	_ = reader.Close()
	writeErr := <-done
	if callErr != nil {
		return nil, unknownOutcome(http.MethodPost, "/api/v2/file-assets", idem, errors.New("上传连接中断"))
	}
	defer res.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if writeErr != nil || readErr != nil || len(raw) > 1<<20 {
		return nil, unknownOutcome(http.MethodPost, "/api/v2/file-assets", idem, errors.New("上传请求或响应不完整"))
	}
	out := &response{status: res.StatusCode, body: raw, requestID: res.Header.Get("X-Request-ID"), etag: res.Header.Get("ETag")}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return out, decodeError(res.StatusCode, raw, idem)
	}
	return out, nil
}
