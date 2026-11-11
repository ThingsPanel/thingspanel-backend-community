package offlineupgrade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"encore.app/devicemetadata/credentialsecret"
	"encore.app/internal/tenantidentity"
)

// CredentialInput preserves existing MQTT authentication material. It does not
// create access bindings, dispatch to a broker, enable devices or authorize cutover.
type CredentialInput struct {
	SourceDatabase      string            `json:"source_database"`
	TargetDatabase      string            `json:"target_database"`
	SourceArchiveSHA256 string            `json:"source_archive_sha256"`
	TargetArchiveSHA256 string            `json:"target_archive_sha256"`
	QuarantineSHA256    string            `json:"quarantine_sha256"`
	RecordedAt          time.Time         `json:"recorded_at"`
	Devices             []json.RawMessage `json:"devices"`
	Templates           []json.RawMessage `json:"templates"`
	ExcludedDevices     []string          `json:"excluded_devices"`
}

type credentialSource struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	ConfigID  *string `json:"device_config_id"`
	ParentID  *string `json:"parent_id"`
	Protocol  *string `json:"protocol"`
	AccessWay *string `json:"access_way"`
	ServiceID *string `json:"service_access_id"`
	Voucher   string  `json:"voucher"`
}
type credentialTemplate struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Protocol    string `json:"protocol_type"`
	Kind        string `json:"device_type"`
	VoucherType string `json:"voucher_type"`
}
type plannedCredential struct {
	SourceID         string
	TenantID         string
	DeviceID         string
	ID               string
	Kind             string
	PublicIdentifier *string
	Secret           []byte
}
type CredentialPlan struct {
	rows        []plannedCredential
	Pending     map[string]int `json:"pending_reasons"`
	Selected    int            `json:"selected"`
	Quarantined int            `json:"quarantined"`
	Actor       string         `json:"-"`
}

func credentialValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func PlanCredentials(in CredentialInput) (CredentialPlan, error) {
	p := CredentialPlan{Pending: map[string]int{}, Actor: "offline-credential-" + in.SourceArchiveSHA256}
	if !rehearsalDatabase.MatchString(in.SourceDatabase) || !rehearsalDatabase.MatchString(in.TargetDatabase) || in.SourceDatabase == in.TargetDatabase || !sha256Pattern.MatchString(in.SourceArchiveSHA256) || !sha256Pattern.MatchString(in.TargetArchiveSHA256) || !sha256Pattern.MatchString(in.QuarantineSHA256) || in.RecordedAt.IsZero() || in.RecordedAt.Nanosecond()%1000 != 0 {
		return p, fmt.Errorf("verified rehearsal receipts and microsecond recording time required")
	}
	templates := map[string]credentialTemplate{}
	for _, raw := range in.Templates {
		var t credentialTemplate
		if json.Unmarshal(raw, &t) != nil || t.ID == "" {
			return p, fmt.Errorf("invalid private template input")
		}
		if _, exists := templates[t.ID]; exists {
			return p, fmt.Errorf("duplicate source template")
		}
		templates[t.ID] = t
	}
	excluded := map[string]bool{}
	for _, id := range in.ExcludedDevices {
		if id == "" || excluded[id] {
			return p, fmt.Errorf("invalid quarantine references")
		}
		excluded[id] = true
	}
	seen := map[string]bool{}
	for _, raw := range in.Devices {
		var d credentialSource
		if json.Unmarshal(raw, &d) != nil || d.ID == "" || seen[d.ID] {
			return p, fmt.Errorf("invalid or duplicate private device input")
		}
		seen[d.ID] = true
		if excluded[d.ID] {
			p.Quarantined++
			continue
		}
		if d.TenantID == "" || strings.TrimSpace(d.TenantID) != d.TenantID {
			return p, fmt.Errorf("device tenant requires explicit reconciliation")
		}
		skip := func(reason string) { p.Pending[reason]++ }
		if credentialValue(d.ServiceID) != "" || (credentialValue(d.AccessWay) != "" && credentialValue(d.AccessWay) != "A") {
			skip("service_connector")
			continue
		}
		if credentialValue(d.ParentID) != "" {
			skip("gateway_child")
			continue
		}
		// Legacy DeviceConnect/GetDeviceVoucherForm explicitly default untemplated
		// devices to MQTT/BASIC. Never apply that default to a configured device.
		protocol, kind, voucherType := "MQTT", "1", "BASIC"
		if id := credentialValue(d.ConfigID); id != "" {
			t, ok := templates[id]
			if !ok || t.TenantID != d.TenantID {
				skip("unresolved_template_ownership")
				continue
			}
			protocol, kind, voucherType = t.Protocol, t.Kind, t.VoucherType
		}
		if protocol != "MQTT" || (credentialValue(d.Protocol) != "" && !strings.EqualFold(credentialValue(d.Protocol), "mqtt")) {
			skip("non_mqtt_protocol")
			continue
		}
		if kind != "1" && kind != "2" {
			skip("gateway_child")
			continue
		}
		secret, username, mode, err := parseLegacyMQTTVoucher(d.Voucher, voucherType)
		if err != nil {
			skip("unsupported_mqtt_voucher")
			continue
		}
		row := plannedCredential{SourceID: d.ID, TenantID: tenantidentity.NormalizePrincipalID(d.TenantID), DeviceID: tenantidentity.CanonicalIdentity("device", d.ID), ID: tenantidentity.CanonicalIdentity("upgrade-device-credential", in.SourceArchiveSHA256+"/"+d.ID), Kind: mode, PublicIdentifier: username, Secret: secret}
		if err := credentialsecret.ValidateCredentialAAD(row.aad()); err != nil {
			return p, fmt.Errorf("invalid canonical credential owner")
		}
		p.rows = append(p.rows, row)
	}
	for id := range excluded {
		if !seen[id] {
			return p, fmt.Errorf("quarantine references absent source device")
		}
	}
	sort.Slice(p.rows, func(i, j int) bool { return p.rows[i].ID < p.rows[j].ID })
	p.Selected = len(p.rows)
	return p, nil
}

func (p plannedCredential) aad() credentialsecret.CredentialAAD {
	return credentialsecret.CredentialAAD{TenantID: p.TenantID, OwnerKind: credentialsecret.CredentialOwnerDevice, OwnerID: p.DeviceID, CredentialID: p.ID, Purpose: credentialsecret.CredentialPurposeDeviceAccess, Use: credentialsecret.CredentialUseMaterial}
}

func parseLegacyMQTTVoucher(raw, kind string) ([]byte, *string, string, error) {
	invalid := fmt.Errorf("unsupported private MQTT voucher")
	if !utf8.ValidString(raw) || len(raw) > 32768 {
		return nil, nil, "", invalid
	}
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, nil, "", invalid
	}
	values := map[string]string{}
	for dec.More() {
		token, err = dec.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, nil, "", invalid
		}
		if _, dup := values[key]; dup {
			return nil, nil, "", invalid
		}
		if key != "username" && key != "password" {
			return nil, nil, "", invalid
		}
		var value string
		if dec.Decode(&value) != nil {
			return nil, nil, "", invalid
		}
		values[key] = value
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') || dec.Decode(new(any)) != io.EOF {
		return nil, nil, "", invalid
	}
	username := values["username"]
	if username == "" {
		return nil, nil, "", invalid
	}
	switch kind {
	case "BASIC":
		password, ok := values["password"]
		if !ok || password == "" || len(password) > 8192 || len([]rune(username)) > 256 {
			return nil, nil, "", invalid
		}
		return []byte(password), &username, "username_password", nil
	case "ACCESSTOKEN":
		// Empty password and absent password are equivalent for MQTT token login.
		if values["password"] != "" || len(username) > 8192 {
			return nil, nil, "", invalid
		}
		return []byte(username), nil, "token", nil
	default:
		return nil, nil, "", invalid
	}
}
