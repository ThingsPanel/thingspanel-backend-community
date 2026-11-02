package platformconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// runtimeConfig explicitly references the existing Community database. It does
// not declare a new Encore-managed database or guess a resource mapping.
type runtimeConfig struct {
	DatabaseURL          string `json:"databaseURL"`
	ExpectedDatabase     string `json:"expectedDatabase"`
	Schema               string `json:"schema"`
	AuthorizationBaseURL string `json:"authorizationBaseURL"`
	MessagesFile         string `json:"messagesFile"`
}

func readConfig(path string) (runtimeConfig, error) {
	var config runtimeConfig
	if !filepath.IsAbs(path) {
		return config, fmt.Errorf("dictionary configuration requires an absolute file path")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 64*1024 {
		return config, fmt.Errorf("dictionary configuration is unavailable")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil {
		return runtimeConfig{}, fmt.Errorf("dictionary configuration is invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return runtimeConfig{}, fmt.Errorf("dictionary configuration has trailing content")
	}
	if strings.TrimSpace(config.DatabaseURL) == "" || strings.TrimSpace(config.ExpectedDatabase) == "" || config.Schema != "public" || strings.TrimSpace(config.AuthorizationBaseURL) == "" || !filepath.IsAbs(config.MessagesFile) {
		return runtimeConfig{}, fmt.Errorf("dictionary configuration is incomplete or unsupported")
	}
	return config, nil
}
