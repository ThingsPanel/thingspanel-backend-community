package brokercontrol

import (
	"context"
	"project/internal/brokercontract"
	"strings"
)

type ConnectionTopic struct {
	Purpose   string `json:"purpose"`
	Direction string `json:"direction"`
	Topic     string `json:"topic"`
}
type ConnectionTopics struct {
	Available      bool              `json:"available"`
	Reason         *string           `json:"reason"`
	MappingVersion *string           `json:"mappingVersion"`
	Items          []ConnectionTopic `json:"items"`
}

func unavailableTopics(reason string) ConnectionTopics {
	return ConnectionTopics{Reason: &reason, Items: []ConnectionTopic{}}
}
func (r *GormRepository) ReadConnectionTopics(ctx context.Context, tenant, id string) (ConnectionTopics, error) {
	rows, err := r.ResolveServiceTargets(ctx, []string{id})
	if err != nil {
		return ConnectionTopics{}, err
	}
	if len(rows) != 1 || rows[0].TenantID != tenant {
		return unavailableTopics("not_ready"), nil
	}
	d := rows[0]
	var protocol string
	if err := r.quietDB(ctx).Table("devices").Select("COALESCE(protocol,'')").Where("tenant_id=? AND id=?", tenant, id).Scan(&protocol).Error; err != nil {
		return ConnectionTopics{}, err
	}
	if !strings.EqualFold(protocol, "mqtt") {
		return unavailableTopics("unsupported_access"), nil
	}
	if d.ParentID != nil && *d.ParentID != "" || d.ServiceAccessID != nil && *d.ServiceAccessID != "" || d.AccessWay != nil && *d.AccessWay == "B" || (d.DeviceType != "" && d.DeviceType != "1") || (d.ProtocolType != nil && *d.ProtocolType != "" && !strings.EqualFold(*d.ProtocolType, "mqtt")) {
		return unavailableTopics("unsupported_access"), nil
	}
	children, err := r.ListEnabledChildren(ctx, tenant, id)
	if err != nil {
		return ConnectionTopics{}, err
	}
	if len(children) > 0 {
		return unavailableTopics("unsupported_access"), nil
	}
	if d.DeviceConfigID != nil {
		custom, err := r.HasEnabledTopicMappings(ctx, tenant, *d.DeviceConfigID)
		if err != nil {
			return ConnectionTopics{}, err
		}
		if custom {
			return unavailableTopics("custom_mapping"), nil
		}
	}
	return standardConnectionTopics(d), nil
}
func standardConnectionTopics(d DeviceRecord) ConnectionTopics {
	if d.DeviceNumber == "" || strings.ContainsAny(d.DeviceNumber, "/+#\x00") {
		return unavailableTopics("not_ready")
	}
	version := "standard-v1"
	out := ConnectionTopics{Available: true, MappingVersion: &version, Items: []ConnectionTopic{}}
	policy := buildDevicePolicy(d, nil, brokercontract.PrincipalDevice)
	for _, direction := range []string{"publish", "subscribe"} {
		topics := policy.Publish
		if direction == "subscribe" {
			topics = policy.Subscribe
		}
		for _, topic := range topics {
			purpose := "telemetry"
			if strings.Contains(topic, "/command/") {
				purpose = "command"
			} else if strings.Contains(topic, "/attributes/") {
				purpose = "attribute"
			} else if strings.Contains(topic, "/event/") {
				purpose = "event"
			}
			if strings.Contains(topic, "/response/") {
				purpose += "_response"
			} else if strings.Contains(topic, "/control/") {
				purpose = "telemetry_control"
			}
			if direction == "publish" {
				topic = strings.ReplaceAll(topic, "+", "{message_id}")
			}
			out.Items = append(out.Items, ConnectionTopic{purpose, direction, topic})
		}
	}
	return out
}
