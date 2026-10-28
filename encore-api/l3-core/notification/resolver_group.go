package notification

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"encore.app/l3-core/notification/contract"
)

var ErrGroupResolution = errors.New("notification_group_resolution_unavailable")

// MemberResolver must use an authorized tenant-scoped legacy adapter. An
// application user ID is never itself a provider chat/user recipient.
type MemberResolver interface {
	ResolveMember(context.Context, string, string, string) ([]contract.Recipient, error)
}

type projectionReader interface {
	ListGroupProjections(context.Context, string, string) ([]contract.SourceGroupProjection, error)
}

type SourceResolver struct {
	Groups         GroupResolver
	Projections    projectionReader
	Members        MemberResolver
	ValidateTarget func(context.Context, string, contract.SubmitDelivery) error
	Now            func() time.Time
}

type recipientBinding struct {
	Kind         string              `json:"kind"`
	Recipient    *contract.Recipient `json:"recipient,omitempty"`
	UserID       *string             `json:"userId,omitempty"`
	ContactField *string             `json:"contactField,omitempty"`
}

type contentBinding struct {
	Kind          string             `json:"kind"`
	Title         *string            `json:"title,omitempty"`
	Text          *string            `json:"text,omitempty"`
	TemplateID    *string            `json:"templateId,omitempty"`
	Locale        *string            `json:"locale,omitempty"`
	ParamsMapping *map[string]string `json:"paramsMapping,omitempty"`
}

// PlanSourceEvent does no submission or sending. A failure in any binding
// returns a blocked plan with zero deliveries, including failures after a
// preceding binding was resolved. Store separately commits the whole plan.
func (r *SourceResolver) PlanSourceEvent(ctx context.Context, tenant string, event contract.SourceEventV1) (SourceAcceptancePlan, error) {
	plan := SourceAcceptancePlan{Event: event}
	blocked := func(reason string) (SourceAcceptancePlan, error) {
		plan.Deliveries = nil
		plan.BlockedReason = reason
		return plan, nil
	}
	if tenant == "" || event.TenantID != tenant || r.Groups == nil || r.Projections == nil || r.ValidateTarget == nil {
		return plan, ErrGroupResolution
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	expires, err := time.Parse(time.RFC3339Nano, event.ExpiresAt)
	if err != nil {
		return plan, ErrGroupResolution
	}
	if !expires.After(now) {
		return blocked("blocked_expired")
	}
	group, err := r.Groups.ResolvePublished(ctx, tenant, event.NotificationGroupID, event.GroupRevision)
	if err != nil {
		var storeError *StoreError
		if errors.As(err, &storeError) && storeError.Kind == StoreErrorNotFound {
			return blocked("group_revision_unavailable")
		}
		return plan, ErrGroupResolution
	}
	if group.ID != event.NotificationGroupID || group.Revision != event.GroupRevision || !group.Enabled {
		return blocked("group_revision_unavailable")
	}
	projections, err := r.Projections.ListGroupProjections(ctx, tenant, group.ID)
	if err != nil {
		return plan, ErrGroupResolution
	}
	mapped := false
	for _, p := range projections {
		if p.TenantID == tenant && p.SourceDeploymentID == event.SourceDeploymentID && p.NotificationGroupID == group.ID && p.GroupRevision == group.Revision {
			mapped = true
			break
		}
	}
	if !mapped {
		return blocked("source_projection_unavailable")
	}
	if len(group.Bindings) == 0 {
		return blocked("group_has_no_targets")
	}
	seen := map[string]bool{}
	for _, binding := range group.Bindings {
		if binding.BindingID == "" || binding.InstanceID == "" {
			return blocked("invalid_group_binding")
		}
		var source recipientBinding
		var contentSpec contentBinding
		if contract.DecodeStrictJSON(binding.RecipientSource, &source) != nil || contract.DecodeStrictJSON(binding.ContentBinding, &contentSpec) != nil {
			return blocked("invalid_group_binding")
		}
		targets := []contract.Recipient{}
		switch source.Kind {
		case "literal":
			if source.Recipient == nil || source.UserID != nil || source.ContactField != nil {
				return blocked("invalid_group_binding")
			}
			targets = append(targets, *source.Recipient)
		case "member":
			if source.Recipient != nil || source.UserID == nil || source.ContactField == nil || strings.TrimSpace(*source.UserID) == "" {
				return blocked("invalid_group_binding")
			}
			if *source.ContactField == "applicationUserId" {
				return blocked("legacy_app_not_migratable")
			}
			if (*source.ContactField != "email" && *source.ContactField != "phone" && *source.ContactField != "appDevice") || r.Members == nil {
				return blocked("member_contact_unavailable")
			}
			targets, err = r.Members.ResolveMember(ctx, tenant, *source.UserID, *source.ContactField)
			if err == nil && len(targets) == 0 && *source.ContactField == "appDevice" {
				continue
			}
			if err != nil || len(targets) == 0 {
				return blocked("member_contact_unavailable")
			}
			for _, target := range targets {
				if target.Kind != *source.ContactField && !(*source.ContactField == "appDevice" && target.Kind == "user_id") {
					return blocked("member_contact_unavailable")
				}
			}
		default:
			return blocked("invalid_group_binding")
		}
		content, err := resolveBindingContent(contentSpec, event.Payload)
		if err != nil {
			return blocked("template_variable_unavailable")
		}
		for _, target := range targets {
			delivery := contract.SubmitDelivery{InstanceID: binding.InstanceID, Recipient: target, Content: content}
			if strings.TrimSpace(target.Address) == "" {
				return blocked("member_contact_unavailable")
			}
			wire, _ := json.Marshal(delivery)
			if seen[string(wire)] {
				continue
			}
			if len(plan.Deliveries) >= 500 {
				return blocked("target_limit_exceeded")
			}
			if err := r.ValidateTarget(ctx, tenant, delivery); err != nil {
				var storeError *StoreError
				if errors.As(err, &storeError) && (storeError.Kind == StoreErrorUnavailable || storeError.Kind == StoreErrorProtectorMissing) {
					return SourceAcceptancePlan{Event: event}, ErrGroupResolution
				}
				return blocked("binding_target_unavailable")
			}
			seen[string(wire)] = true
			plan.Deliveries = append(plan.Deliveries, delivery)
		}
	}
	if len(plan.Deliveries) == 0 {
		return blocked("app_no_active_devices")
	}
	encoded, err := json.Marshal(plan.Deliveries)
	if err != nil {
		return plan, ErrGroupResolution
	}
	if len(encoded) > 1<<20 {
		return blocked("expanded_intent_limit_exceeded")
	}
	return plan, nil
}

var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.$-]+)\s*\}\}`)

func renderNotificationText(template string, payload contract.SourceEventPayload) (string, error) {
	indices := placeholder.FindAllStringSubmatchIndex(template, -1)
	var out strings.Builder
	last := 0
	for _, index := range indices {
		literal := template[last:index[0]]
		if strings.Contains(literal, "{{") || strings.Contains(literal, "}}") {
			return "", ErrGroupResolution
		}
		out.WriteString(literal)
		value, ok := notificationVariable(template[index[2]:index[3]], payload)
		if !ok {
			return "", ErrGroupResolution
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			var scalar any
			if json.Unmarshal(value, &scalar) != nil || scalar == nil {
				return "", ErrGroupResolution
			}
			switch scalar.(type) {
			case bool, float64:
				text = string(value)
			default:
				return "", ErrGroupResolution
			}
		}
		if out.Len()+len(text) > 64<<10 {
			return "", ErrGroupResolution
		}
		out.WriteString(text)
		last = index[1]
	}
	if out.Len()+len(template[last:]) > 64<<10 {
		return "", ErrGroupResolution
	}
	if strings.Contains(template[last:], "{{") || strings.Contains(template[last:], "}}") {
		return "", ErrGroupResolution
	}
	out.WriteString(template[last:])
	return out.String(), nil
}

func notificationVariable(name string, payload contract.SourceEventPayload) (json.RawMessage, bool) {
	if raw, ok := payload.Variables[name]; ok {
		return append(json.RawMessage(nil), raw...), true
	}
	name = strings.TrimPrefix(name, "$.")
	name = strings.TrimPrefix(name, "payload.")
	switch name {
	case "subject":
		raw, _ := json.Marshal(payload.Subject)
		return raw, true
	case "text":
		raw, _ := json.Marshal(payload.Text)
		return raw, true
	case "legacyAlertJson":
		if payload.LegacyAlertJSON != nil {
			raw, _ := json.Marshal(payload.LegacyAlertJSON)
			return raw, true
		}
	}
	name = strings.TrimPrefix(name, "variables.")
	if raw, ok := payload.Variables[name]; ok {
		return append(json.RawMessage(nil), raw...), true
	}
	parts := strings.Split(name, ".")
	if len(parts) == 0 {
		return nil, false
	}
	raw, ok := payload.Variables[parts[0]]
	if !ok {
		return nil, false
	}
	for _, part := range parts[1:] {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			return nil, false
		}
		raw, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return append(json.RawMessage(nil), raw...), true
}

func resolveBindingContent(binding contentBinding, payload contract.SourceEventPayload) (contract.Content, error) {
	content := contract.Content{Kind: binding.Kind, Title: binding.Title}
	if binding.Title != nil {
		rendered, err := renderNotificationText(*binding.Title, payload)
		if err != nil {
			return content, err
		}
		content.Title = &rendered
	}
	switch binding.Kind {
	case "text":
		if binding.Title == nil || binding.Text == nil || binding.TemplateID != nil || binding.Locale != nil || binding.ParamsMapping != nil {
			return content, ErrGroupResolution
		}
		rendered, err := renderNotificationText(*binding.Text, payload)
		if err != nil || strings.TrimSpace(rendered) == "" {
			return content, ErrGroupResolution
		}
		content.Text = &rendered
	case "template":
		if binding.Title != nil || binding.Text != nil || binding.TemplateID == nil || strings.TrimSpace(*binding.TemplateID) == "" || binding.Locale == nil || binding.ParamsMapping == nil {
			return content, ErrGroupResolution
		}
		params := map[string]json.RawMessage{}
		for key, path := range *binding.ParamsMapping {
			raw, ok := notificationVariable(path, payload)
			if key == "" || !ok {
				return content, ErrGroupResolution
			}
			params[key] = raw
		}
		content.Template = &contract.TemplateContent{ID: *binding.TemplateID, Locale: binding.Locale, Params: params}
	default:
		return content, ErrGroupResolution
	}
	return content, nil
}
