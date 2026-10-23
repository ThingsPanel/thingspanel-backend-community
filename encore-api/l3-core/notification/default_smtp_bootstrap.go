package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"

	"encore.app/l3-core/notification/contract"
)

const defaultSMTPPluginID = "thingspanel.smtp"

var errDefaultSMTPBootstrap = errors.New("default SMTP plugin bootstrap is unavailable")

type defaultSMTPBootstrapConfig struct {
	enabled bool
	origin  string
	token   string
}

func defaultSMTPBootstrapConfigFromEnv(getenv func(string) string) (defaultSMTPBootstrapConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	mode := strings.TrimSpace(getenv("NOTIFICATION_DEFAULT_SMTP_BOOTSTRAP"))
	if mode == "" || mode == "false" {
		return defaultSMTPBootstrapConfig{}, nil
	}
	if mode != "true" {
		return defaultSMTPBootstrapConfig{}, errDefaultSMTPBootstrap
	}
	origin := strings.TrimSpace(getenv("NOTIFICATION_DEFAULT_SMTP_PLUGIN_ORIGIN"))
	token := getenv("NOTIFICATION_DEFAULT_SMTP_PLUGIN_TOKEN")
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" || token == "" || len(token) > 8192 || strings.TrimSpace(token) != token || strings.ContainsAny(token, "\r\n\x00") {
		return defaultSMTPBootstrapConfig{}, errDefaultSMTPBootstrap
	}
	return defaultSMTPBootstrapConfig{enabled: true, origin: origin, token: token}, nil
}

// BootstrapDefaultSMTP ensures the fixed SMTP adapter is present in the global
// plugin directory. It deliberately creates no tenant grants or instances and
// never changes an existing registration or its encrypted auth secret.
func (s *ManagementService) BootstrapDefaultSMTP(ctx context.Context, cfg defaultSMTPBootstrapConfig) error {
	if !cfg.enabled {
		return nil
	}
	if s == nil || s.platformPlugins == nil || s.pluginHTTP == nil || s.privacy == nil || s.privacy.Check() != nil || !validPluginOriginShape(cfg.origin) || cfg.token == "" || len(cfg.token) > 8192 || strings.TrimSpace(cfg.token) != cfg.token || strings.ContainsAny(cfg.token, "\r\n\x00") {
		return errDefaultSMTPBootstrap
	}
	registered, err := s.platformPlugins.ListPlatformPlugins(ctx)
	if err != nil {
		return errDefaultSMTPBootstrap
	}
	for _, item := range registered {
		if item.PluginID == defaultSMTPPluginID {
			return nil
		}
	}

	maxManifestBytes := s.options.ManifestMaxBytes
	if maxManifestBytes <= 0 || maxManifestBytes > 1<<20 {
		maxManifestBytes = 256 << 10
	}
	raw, manifest, err := s.pluginHTTP.FetchManifest(ctx, cfg.origin, cfg.token)
	if err != nil || len(raw) == 0 || int64(len(raw)) > maxManifestBytes || manifest.PluginID != defaultSMTPPluginID || validatePluginManifest(manifest) != nil {
		return errDefaultSMTPBootstrap
	}
	digest := pluginManifestDigest(raw)
	request := contract.PluginRegistration{
		PluginID: defaultSMTPPluginID, PluginVersion: manifest.PluginVersion,
		Origin: cfg.origin, AuthSecret: cfg.token, ManifestDigest: digest,
	}
	requestHash := sha256.Sum256([]byte("default-smtp-bootstrap-v1\n" + cfg.origin + "\n" + digest))
	_, err = s.platformPlugins.RegisterPlatformPlugin(ctx, "platform-default-smtp-bootstrap", "default-smtp-bootstrap-v1", hex.EncodeToString(requestHash[:]), request, manifest)
	if err != nil {
		return errDefaultSMTPBootstrap
	}
	return nil
}
