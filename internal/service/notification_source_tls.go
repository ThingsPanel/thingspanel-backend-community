package service

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// LoadSourceBridgeRoots adds an operator-provided CA bundle to the platform
// trust pool. It never disables certificate or hostname verification.
func LoadSourceBridgeRoots(path string) (*x509.CertPool, error) {
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("invalid source CA path")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 1<<20 {
		return nil, errors.New("invalid source CA file")
	}
	data, err := os.ReadFile(path)
	if err != nil || !validSourceCABundle(data) {
		return nil, errors.New("invalid source CA bundle")
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil || !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("system trust roots unavailable")
	}
	return pool, nil
}

func validSourceCABundle(data []byte) bool {
	remaining := bytes.TrimSpace(data)
	count := 0
	for len(remaining) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return false
		}
		certs, err := x509.ParseCertificates(block.Bytes)
		if err != nil || len(certs) != 1 || !certs[0].IsCA {
			return false
		}
		count++
		remaining = bytes.TrimSpace(rest)
	}
	return count > 0
}
