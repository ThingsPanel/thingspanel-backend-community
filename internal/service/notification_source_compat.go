package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"project/internal/dal"
	"project/internal/model"
)

const (
	sourceGroupSnapshotPath = "/api/v1/notification-source/group-snapshots/"
	sourceSnapshotMaxBody   = 1 << 20
	sourceSMTPPluginID      = "thingspanel.smtp"
	legacyEmailTextTemplate = "{{text}}\n\n---\nThis email was sent by ThingsPanel"
)

// EmailSourceCompatibilityChecker proves that every old EMAIL recipient maps
// to one enabled SMTP target in the exact enabled native group revision.
type EmailSourceCompatibilityChecker struct {
	client        *http.Client
	fetchSnapshot func(context.Context, SourceGroupProjectionRequest) (sourceGroupSnapshot, error)
	legacyConfig  func(context.Context) (model.EmailConfig, bool, error)
}

func NewEmailSourceCompatibilityChecker(baseURL, projectionBearerToken string) (*EmailSourceCompatibilityChecker, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed == nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || !validSourceProjectionToken(projectionBearerToken) {
		return nil, errors.New("source compatibility configuration invalid")
	}
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          4,
			MaxIdleConnsPerHost:   1,
			IdleConnTimeout:       10 * time.Second,
			ResponseHeaderTimeout: 3 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &EmailSourceCompatibilityChecker{
		client: client,
		fetchSnapshot: func(ctx context.Context, request SourceGroupProjectionRequest) (sourceGroupSnapshot, error) {
			return fetchSourceGroupSnapshot(ctx, client, parsed, projectionBearerToken, request)
		},
		legacyConfig: loadLegacyEmailConfig,
	}, nil
}

func (c *EmailSourceCompatibilityChecker) Close() {
	if c != nil && c.client != nil {
		if transport, ok := c.client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
}

func validSourceProjectionToken(token string) bool {
	if len(token) < 32 || len(token) > 8192 {
		return false
	}
	for _, char := range token {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func (c *EmailSourceCompatibilityChecker) CheckEncoreCompatibility(ctx context.Context, group *model.NotificationGroup, request SourceGroupProjectionRequest) error {
	if c == nil || group == nil || c.fetchSnapshot == nil || c.legacyConfig == nil || request.SourceDeploymentID == "" || request.TenantID == "" || request.LegacyGroupID == "" || request.NotificationGroupID == "" || request.GroupRevision < 1 || request.LegacyGroupID != group.ID || request.TenantID != group.TenantID || group.Status != "OPEN" || group.NotificationType != model.NoticeType_Email {
		return errors.New("source group is not compatible")
	}
	legacyRecipients, err := legacyEmailRecipients(group)
	if err != nil {
		return errors.New("source group is not compatible")
	}
	legacyConfig, legacyEnabled, err := c.legacyConfig(ctx)
	if err != nil || !legacyEnabled {
		return errors.New("source group is not compatible")
	}
	snapshot, err := c.fetchSnapshot(ctx, request)
	if err != nil || snapshot.SourceDeploymentID != request.SourceDeploymentID || snapshot.TenantID != request.TenantID || snapshot.Group.ID != request.NotificationGroupID || !snapshot.Group.Enabled || snapshot.Group.Revision != request.GroupRevision {
		return errors.New("source group is not compatible")
	}
	if err := compareSourceEmailProjection(snapshot, legacyRecipients, legacyConfig); err != nil {
		return errors.New("source group is not compatible")
	}
	return nil
}

type sourceGroupSnapshotEnvelope struct {
	Code      int                 `json:"code"`
	Message   string              `json:"message"`
	RequestID string              `json:"requestId"`
	Data      sourceGroupSnapshot `json:"data"`
}

type sourceGroupSnapshot struct {
	SourceDeploymentID string                   `json:"sourceDeploymentId"`
	TenantID           string                   `json:"tenantId"`
	Group              sourceSnapshotGroup      `json:"group"`
	Instances          []sourceSnapshotInstance `json:"instances"`
	Plugins            []sourceSnapshotPlugin   `json:"plugins"`
}

type sourceSnapshotGroup struct {
	ID       string                  `json:"id"`
	Enabled  bool                    `json:"enabled"`
	Revision int64                   `json:"revision"`
	Bindings []sourceSnapshotBinding `json:"bindings"`
}

type sourceSnapshotBinding struct {
	BindingID       string          `json:"bindingId"`
	InstanceID      string          `json:"instanceId"`
	RecipientSource json.RawMessage `json:"recipientSource"`
	ContentBinding  json.RawMessage `json:"contentBinding"`
}

type sourceSnapshotInstance struct {
	ID                   string                     `json:"id"`
	PluginRegistrationID string                     `json:"pluginRegistrationId"`
	Channel              string                     `json:"channel"`
	Enabled              bool                       `json:"enabled"`
	Config               map[string]json.RawMessage `json:"config"`
	SecretState          map[string]bool            `json:"secretState"`
	ProviderIdentity     map[string]string          `json:"providerIdentity"`
	ConfigVersion        int64                      `json:"configVersion"`
}

type sourceSnapshotPlugin struct {
	ID            string `json:"id"`
	PluginID      string `json:"pluginId"`
	PluginVersion string `json:"pluginVersion"`
	Enabled       bool   `json:"enabled"`
}

func fetchSourceGroupSnapshot(ctx context.Context, client *http.Client, base *url.URL, token string, request SourceGroupProjectionRequest) (sourceGroupSnapshot, error) {
	var empty sourceGroupSnapshot
	if client == nil || base == nil || request.TenantID == "" || request.NotificationGroupID == "" || request.GroupRevision < 1 {
		return empty, errors.New("source snapshot unavailable")
	}
	endpoint := *base
	endpoint.Path = strings.TrimRight(base.Path, "/") + sourceGroupSnapshotPath + request.NotificationGroupID
	endpoint.RawPath = strings.TrimRight(base.EscapedPath(), "/") + sourceGroupSnapshotPath + url.PathEscape(request.NotificationGroupID)
	query := url.Values{}
	query.Set("tenantId", request.TenantID)
	query.Set("groupRevision", strconv.FormatInt(request.GroupRevision, 10))
	endpoint.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return empty, errors.New("source snapshot unavailable")
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(httpRequest)
	if err != nil {
		return empty, errors.New("source snapshot unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !isJSONResponse(response) {
		return empty, errors.New("source snapshot unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, sourceSnapshotMaxBody+1))
	if err != nil || len(body) == 0 || len(body) > sourceSnapshotMaxBody {
		return empty, errors.New("source snapshot invalid")
	}
	var envelope sourceGroupSnapshotEnvelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || envelope.Code != 200 || strings.TrimSpace(envelope.Message) == "" || strings.TrimSpace(envelope.RequestID) == "" {
		return empty, errors.New("source snapshot invalid")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return empty, errors.New("source snapshot invalid")
	}
	if !sortedUniqueIDs(groupIDs(envelope.Data)) || !sortedUniqueIDs(instanceIDs(envelope.Data.Instances)) || !sortedUniqueIDs(pluginIDs(envelope.Data.Plugins)) {
		return empty, errors.New("source snapshot invalid")
	}
	return envelope.Data, nil
}

func sortedUniqueIDs(ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	for i, id := range ids {
		if id == "" || (i > 0 && ids[i-1] >= id) {
			return false
		}
	}
	return true
}

func groupIDs(snapshot sourceGroupSnapshot) []string {
	ids := make([]string, len(snapshot.Group.Bindings))
	for i := range snapshot.Group.Bindings {
		ids[i] = snapshot.Group.Bindings[i].BindingID
	}
	return ids
}

func instanceIDs(instances []sourceSnapshotInstance) []string {
	ids := make([]string, len(instances))
	for i := range instances {
		ids[i] = instances[i].ID
	}
	return ids
}

func pluginIDs(plugins []sourceSnapshotPlugin) []string {
	ids := make([]string, len(plugins))
	for i := range plugins {
		ids[i] = plugins[i].ID
	}
	return ids
}

func loadLegacyEmailConfig(ctx context.Context) (model.EmailConfig, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.EmailConfig{}, false, err
	}
	stored, err := dal.GetNotificationServicesConfigByType(model.NoticeType_Email)
	if err != nil || stored == nil || stored.Status != "OPEN" || stored.Config == nil || len(*stored.Config) == 0 || len(*stored.Config) > 16<<10 {
		return model.EmailConfig{}, false, errors.New("legacy email configuration unavailable")
	}
	decoder := json.NewDecoder(strings.NewReader(*stored.Config))
	decoder.DisallowUnknownFields()
	var config model.EmailConfig
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF || config.Host == "" || config.Port < 1 || config.Port > 65535 || config.FromPassword == "" || normalizeMailbox(config.FromEmail) == "" {
		return model.EmailConfig{}, false, errors.New("legacy email configuration invalid")
	}
	return config, true, nil
}

func legacyEmailRecipients(group *model.NotificationGroup) ([]string, error) {
	if group == nil || group.NotificationType != model.NoticeType_Email || group.NotificationConfig == nil || len(*group.NotificationConfig) == 0 || len(*group.NotificationConfig) > 16<<10 {
		return nil, errors.New("legacy email recipients unavailable")
	}
	var config map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(*group.NotificationConfig))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF || len(config) != 1 {
		return nil, errors.New("legacy email recipients invalid")
	}
	var raw string
	if json.Unmarshal(config["EMAIL"], &raw) != nil || strings.TrimSpace(raw) == "" {
		return nil, errors.New("legacy email recipients invalid")
	}
	parts := strings.Split(raw, ",")
	recipients := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		normalized := normalizeMailbox(strings.TrimSpace(part))
		if normalized == "" {
			return nil, errors.New("legacy email recipients invalid")
		}
		if _, duplicate := seen[normalized]; duplicate {
			return nil, errors.New("legacy email recipients invalid")
		}
		seen[normalized] = struct{}{}
		recipients = append(recipients, normalized)
	}
	sort.Strings(recipients)
	return recipients, nil
}

func normalizeMailbox(value string) string {
	if value == "" || strings.TrimSpace(value) != value {
		return ""
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || parsed.Name != "" || strings.ContainsAny(value, "\r\n\t ,") {
		return ""
	}
	at := strings.LastIndexByte(value, '@')
	if at <= 0 || at == len(value)-1 {
		return ""
	}
	local, domain := value[:at], value[at+1:]
	if strings.TrimSpace(local) != local || strings.TrimSpace(domain) != domain || strings.Contains(domain, "..") || strings.Contains(local, "..") {
		return ""
	}
	return local + "@" + strings.ToLower(domain)
}

func compareSourceEmailProjection(snapshot sourceGroupSnapshot, recipients []string, legacy model.EmailConfig) error {
	if len(snapshot.Group.Bindings) == 0 || len(recipients) == 0 || len(snapshot.Group.Bindings) != len(recipients) {
		return errors.New("source binding set mismatch")
	}
	instances := make(map[string]sourceSnapshotInstance, len(snapshot.Instances))
	for _, instance := range snapshot.Instances {
		if instance.ID == "" || instance.PluginRegistrationID == "" || instances[instance.ID].ID != "" {
			return errors.New("source instance set invalid")
		}
		instances[instance.ID] = instance
	}
	plugins := make(map[string]sourceSnapshotPlugin, len(snapshot.Plugins))
	for _, plugin := range snapshot.Plugins {
		if plugin.ID == "" || plugins[plugin.ID].ID != "" {
			return errors.New("source plugin set invalid")
		}
		plugins[plugin.ID] = plugin
	}
	addresses := make([]string, 0, len(snapshot.Group.Bindings))
	seenAddresses := make(map[string]struct{}, len(snapshot.Group.Bindings))
	seenBindings := make(map[string]struct{}, len(snapshot.Group.Bindings))
	usedInstances := make(map[string]struct{}, len(snapshot.Group.Bindings))
	usedPlugins := make(map[string]struct{}, len(snapshot.Group.Bindings))
	for _, binding := range snapshot.Group.Bindings {
		if binding.BindingID == "" || binding.InstanceID == "" {
			return errors.New("source binding invalid")
		}
		if _, duplicate := seenBindings[binding.BindingID]; duplicate {
			return errors.New("source binding invalid")
		}
		seenBindings[binding.BindingID] = struct{}{}
		var recipient struct {
			Kind      string `json:"kind"`
			Recipient struct {
				Kind    string `json:"kind"`
				Address string `json:"address"`
			} `json:"recipient"`
		}
		if strictSnapshotJSON(binding.RecipientSource, &recipient) != nil || recipient.Kind != "literal" || recipient.Recipient.Kind != "email" {
			return errors.New("source recipient invalid")
		}
		address := normalizeMailbox(recipient.Recipient.Address)
		if address == "" {
			return errors.New("source recipient invalid")
		}
		if _, duplicate := seenAddresses[address]; duplicate {
			return errors.New("source recipient invalid")
		}
		seenAddresses[address] = struct{}{}
		addresses = append(addresses, address)
		var content struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
			Text  string `json:"text"`
		}
		if strictSnapshotJSON(binding.ContentBinding, &content) != nil || content.Kind != "text" || content.Title != "{{subject}}" || content.Text != legacyEmailTextTemplate {
			return errors.New("source content binding invalid")
		}
		instance, ok := instances[binding.InstanceID]
		if !ok || !instance.Enabled || instance.Channel != "email" || instance.SecretState["from_password"] != true || instance.ConfigVersion < 1 {
			return errors.New("source instance unavailable")
		}
		plugin, ok := plugins[instance.PluginRegistrationID]
		if !ok || !plugin.Enabled || plugin.PluginID != sourceSMTPPluginID || plugin.PluginVersion == "" {
			return errors.New("source plugin unavailable")
		}
		if err := compareSMTPIdentity(instance, legacy); err != nil {
			return err
		}
		usedInstances[instance.ID] = struct{}{}
		usedPlugins[plugin.ID] = struct{}{}
	}
	if len(usedInstances) != len(instances) || len(usedPlugins) != len(plugins) {
		return errors.New("source snapshot contains unbound targets")
	}
	sort.Strings(addresses)
	if len(addresses) != len(recipients) {
		return errors.New("source recipient set mismatch")
	}
	for i := range addresses {
		if addresses[i] != recipients[i] {
			return errors.New("source recipient set mismatch")
		}
	}
	return nil
}

func strictSnapshotJSON(raw json.RawMessage, out any) error {
	if len(raw) == 0 || len(raw) > 16<<10 {
		return errors.New("source snapshot invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("source snapshot invalid")
	}
	return nil
}

func compareSMTPIdentity(instance sourceSnapshotInstance, legacy model.EmailConfig) error {
	if len(instance.Config) != 3 && len(instance.Config) != 4 {
		return errors.New("source smtp config invalid")
	}
	for key := range instance.Config {
		if key != "host" && key != "port" && key != "from_email" && key != "ssl" {
			return errors.New("source smtp config invalid")
		}
	}
	var host, fromEmail string
	var port int
	var ssl bool
	if json.Unmarshal(instance.Config["host"], &host) != nil || host == "" || json.Unmarshal(instance.Config["port"], &port) != nil || port < 1 || port > 65535 || json.Unmarshal(instance.Config["from_email"], &fromEmail) != nil || normalizeMailbox(fromEmail) == "" {
		return errors.New("source smtp config invalid")
	}
	if raw, exists := instance.Config["ssl"]; exists {
		trimmed := strings.TrimSpace(string(raw))
		if trimmed != "true" && trimmed != "false" {
			return errors.New("source smtp config invalid")
		}
		ssl = trimmed == "true"
	}
	legacySSL := legacy.SSL != nil && *legacy.SSL
	if host != legacy.Host || port != legacy.Port || normalizeMailbox(fromEmail) != normalizeMailbox(legacy.FromEmail) || ssl != legacySSL {
		return errors.New("source smtp identity mismatch")
	}
	return nil
}

// LegacyEmailImportPlan is a local operator preview only. It contains no
// secret material and never persists a group or sends a notification.
type LegacyEmailImportPlan struct {
	Name     string               `json:"name"`
	Enabled  bool                 `json:"enabled"`
	Bindings []LegacyEmailBinding `json:"bindings"`
}

type LegacyEmailBinding struct {
	BindingID       string                 `json:"bindingId"`
	InstanceID      string                 `json:"instanceId"`
	RecipientSource map[string]interface{} `json:"recipientSource"`
	ContentBinding  map[string]string      `json:"contentBinding"`
}

type VerifiedSMTPImportTarget struct {
	ID       string
	TenantID string
	PluginID string
	Channel  string
	Enabled  bool
}

// PlanLegacyEmailGroupImport only builds a disabled request body after a
// caller has independently verified the tenant-owned SMTP target.
func PlanLegacyEmailGroupImport(group *model.NotificationGroup, tenantID, name string, target VerifiedSMTPImportTarget) (LegacyEmailImportPlan, error) {
	var plan LegacyEmailImportPlan
	if group == nil || group.TenantID == "" || tenantID != group.TenantID || group.Status != "OPEN" || group.NotificationType != model.NoticeType_Email || strings.TrimSpace(name) == "" || target.ID == "" || target.TenantID != tenantID || target.PluginID != sourceSMTPPluginID || target.Channel != "email" || !target.Enabled {
		return plan, errors.New("legacy email import is not compatible")
	}
	recipients, err := legacyEmailRecipients(group)
	if err != nil {
		return plan, errors.New("legacy email import is not compatible")
	}
	plan.Name = name
	plan.Enabled = false
	for index, recipient := range recipients {
		plan.Bindings = append(plan.Bindings, LegacyEmailBinding{
			BindingID:  fmt.Sprintf("legacy-email-%04d", index+1),
			InstanceID: target.ID,
			RecipientSource: map[string]interface{}{
				"kind": "literal", "recipient": map[string]string{"kind": "email", "address": recipient},
			},
			ContentBinding: map[string]string{"kind": "text", "title": "{{subject}}", "text": legacyEmailTextTemplate},
		})
	}
	return plan, nil
}

// PlanLegacyEmailGroupImportDraft creates the disabled body for review before
// the native group exists. The target ID and metadata are assumptions only;
// SwitchToEncore must later verify the complete published group snapshot.
func PlanLegacyEmailGroupImportDraft(group *model.NotificationGroup, tenantID, name, targetInstanceID string) (LegacyEmailImportPlan, error) {
	return PlanLegacyEmailGroupImport(group, tenantID, name, VerifiedSMTPImportTarget{
		ID:       targetInstanceID,
		TenantID: tenantID,
		PluginID: sourceSMTPPluginID,
		Channel:  "email",
		Enabled:  true,
	})
}
