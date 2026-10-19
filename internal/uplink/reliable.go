package uplink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"project/internal/brokercommands"
	"project/internal/brokerledger"
	"project/internal/model"
	"project/internal/processor"
	"project/internal/storage"
)

// ReliableProcessor reuses the community decoder and domain storage, but writes
// business rows and automation intents synchronously inside the receipt's DB
// transaction. It does not send a bus message or start an untracked goroutine.
type ReliableProcessor struct {
	decoder processor.DataProcessor
	logger  *logrus.Logger
}

func NewReliableProcessor(logger *logrus.Logger) *ReliableProcessor {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &ReliableProcessor{decoder: processor.NewScriptProcessor(), logger: logger}
}
func (p *ReliableProcessor) Process(ctx context.Context, tx *gorm.DB, msg brokerledger.Message) error {
	if tx == nil {
		return errors.New("reliable ingress transaction is nil")
	}
	var device model.Device
	if e := tx.WithContext(ctx).Where("id=? AND tenant_id=? AND is_enabled='enabled'", msg.DeviceID, msg.TenantID).Take(&device).Error; e != nil {
		return e
	}
	if strings.HasSuffix(msg.Stream, "response") {
		return p.response(ctx, tx, &device, msg)
	}
	data := msg.Payload
	family := strings.TrimPrefix(msg.Stream, "gateway_")
	if family != "event" && family != "attribute" {
		return fmt.Errorf("unsupported reliable business stream %q", msg.Stream)
	}
	if device.DeviceConfigID != nil && *device.DeviceConfigID != "" {
		output, e := p.decoder.Decode(ctx, &processor.DecodeInput{DeviceConfigID: *device.DeviceConfigID, Type: processor.DataType(family), RawData: data, Timestamp: messageTime(msg).UnixMilli()})
		if e != nil {
			return e
		}
		if output == nil || !output.Success {
			return errors.New("reliable ingress decoder failed")
		}
		data = output.Data
	}
	count := 0
	return p.expand(ctx, tx, &device, msg, family, data, strings.HasPrefix(msg.Stream, "gateway_"), 0, &count)
}
func messageTime(msg brokerledger.Message) time.Time {
	switch v := msg.Metadata["timestamp"].(type) {
	case int64:
		return time.UnixMilli(v)
	case float64:
		return time.UnixMilli(int64(v))
	case json.Number:
		if n, e := v.Int64(); e == nil {
			return time.UnixMilli(n)
		}
	}
	return time.Now().UTC()
}

// expand retains gateway/child ownership in the same tenant at every recursion.
func (p *ReliableProcessor) expand(ctx context.Context, tx *gorm.DB, device *model.Device, msg brokerledger.Message, family string, data []byte, gateway bool, depth int, count *int) error {
	if depth > 8 || *count >= 1000 {
		return errors.New("gateway ingress expansion limit")
	}
	*count++
	if !gateway {
		return p.write(ctx, tx, device, msg, family, data)
	}
	var frame struct {
		GatewayData    json.RawMessage            `json:"gateway_data"`
		SubDeviceData  map[string]json.RawMessage `json:"sub_device_data"`
		SubGatewayData map[string]json.RawMessage `json:"sub_gateway_data"`
	}
	if e := json.Unmarshal(data, &frame); e != nil {
		return e
	}
	if len(frame.GatewayData) > 0 && string(frame.GatewayData) != "null" {
		if e := p.write(ctx, tx, device, msg, family, frame.GatewayData); e != nil {
			return e
		}
	}
	for _, group := range []struct {
		children  map[string]json.RawMessage
		isGateway bool
	}{{frame.SubDeviceData, false}, {frame.SubGatewayData, true}} {
		for address, payload := range group.children {
			var child model.Device
			if e := tx.WithContext(ctx).Where("parent_id=? AND tenant_id=? AND sub_device_addr=? AND is_enabled='enabled'", device.ID, msg.TenantID, address).Take(&child).Error; e != nil {
				return e
			}
			if e := p.expand(ctx, tx, &child, msg, family, payload, group.isGateway, depth+1, count); e != nil {
				return e
			}
		}
	}
	return nil
}
func (p *ReliableProcessor) write(ctx context.Context, tx *gorm.DB, device *model.Device, msg brokerledger.Message, family string, data []byte) error {
	writer := storage.NewDirectWriter(tx, p.logger)
	values := map[string]interface{}{}
	var trigger string
	if family == "event" {
		var event model.EventInfo
		if e := json.Unmarshal(data, &event); e != nil {
			var raw interface{}
			if json.Unmarshal(data, &raw) != nil {
				raw = string(data)
			}
			event = model.EventInfo{Method: "_raw", Params: map[string]interface{}{"value": raw}}
		}
		params, e := json.Marshal(event.Params)
		if e != nil {
			return e
		}
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(msg.ID+"/"+device.ID+"/event")).String()
		if e = writer.WriteEventData(ctx, &storage.EventDataModel{ID: id, DeviceID: device.ID, TenantID: msg.TenantID, Identify: event.Method, TS: messageTime(msg), Data: params}); e != nil {
			return e
		}
		values[event.Method] = string(params)
		trigger = model.TRIGGER_PARAM_TYPE_EVT
	} else {
		if e := json.Unmarshal(data, &values); e != nil {
			var raw interface{}
			if json.Unmarshal(data, &raw) != nil {
				raw = string(data)
			}
			values = map[string]interface{}{"_raw": raw}
		}
		for key, value := range values {
			b, n, s := reliableValue(value)
			id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(msg.ID+"/"+device.ID+"/attribute/"+key)).String()
			if e := writer.WriteAttributeDataIfNewer(ctx, &storage.AttributeData{ID: id, DeviceID: device.ID, TenantID: msg.TenantID, Key: key, TS: messageTime(msg), BoolV: b, NumberV: n, StringV: s}); e != nil {
				return e
			}
		}
		trigger = model.TRIGGER_PARAM_TYPE_ATTR
	}
	intent, e := json.Marshal(map[string]interface{}{"trigger_param_type": trigger, "trigger_values": values, "route_epoch": msg.RouteEpoch, "broker_cell": msg.BrokerCell})
	if e != nil {
		return e
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(msg.ID+"/"+device.ID+"/automation")).String()
	if e := tx.WithContext(ctx).Exec(`INSERT INTO broker_ingress_effects(id,receipt_id,tenant_id,device_id,effect_kind,payload) VALUES(?,?,?,?,?,?::jsonb) ON CONFLICT(receipt_id,device_id,effect_kind) DO NOTHING`, id, msg.ID, msg.TenantID, device.ID, "automation", string(intent)).Error; e != nil {
		return e
	}
	return brokerledger.CheckEffectCapacity(tx.WithContext(ctx), msg.TenantID)
}
func reliableValue(value interface{}) (*bool, *float64, *string) {
	switch v := value.(type) {
	case bool:
		return &v, nil, nil
	case float64:
		return nil, &v, nil
	case string:
		return nil, nil, &v
	case nil:
		s := "null"
		return nil, nil, &s
	default:
		b, e := json.Marshal(v)
		s := string(b)
		if e != nil {
			s = fmt.Sprint(v)
		}
		return nil, nil, &s
	}
}
func (p *ReliableProcessor) response(ctx context.Context, tx *gorm.DB, device *model.Device, msg brokerledger.Message) error {
	id, _ := msg.Metadata["message_id"].(string)
	if id == "" {
		return errors.New("response missing message_id")
	}
	var ack struct {
		Result  *int   `json:"result"`
		Message string `json:"message"`
		Errcode string `json:"errcode"`
	}
	if e := json.Unmarshal(msg.Payload, &ack); e != nil {
		return e
	}
	if ack.Result == nil || (*ack.Result != 0 && *ack.Result != 1) {
		return errors.New("invalid response result")
	}
	status := "3"
	failure := ""
	if *ack.Result == 1 {
		status = "4"
		failure = ack.Errcode
		if failure == "" {
			failure = ack.Message
		}
	}
	table := "command_set_logs"
	if strings.Contains(msg.Stream, "attribute_set_response") {
		table = "attribute_set_logs"
	}
	logicalDevice := device.ID
	if table == "command_set_logs" {
		row, err := brokercommands.New(tx).GetScoped(ctx, msg.TenantID, id)
		if err == nil {
			if row.TargetDeviceID != device.ID || row.RouteEpoch <= 0 || uint64(row.RouteEpoch) != msg.RouteEpoch {
				return errors.New("response command route scope mismatch")
			}
			logicalDevice = row.DeviceID
			err = brokercommands.New(tx).Acknowledge(ctx, tx, msg.TenantID, device.ID, id, *ack.Result == 0)
			if errors.Is(err, brokercommands.ErrState) {
				want := brokercommands.StateDeviceReportedSuccess
				if *ack.Result != 0 {
					want = brokercommands.StateDeviceReportedFailure
				}
				if row.State == want {
					return nil
				}
			}
			if err != nil {
				return err
			}
		} else if !errors.Is(err, brokercommands.ErrNotFound) {
			return err
		}
	}
	// The authenticated device remains the scope even if payload IDs disagree.
	result := tx.WithContext(ctx).Table(table).Where("device_id=? AND message_id=? AND status IN ?", logicalDevice, id, []string{"0", "1", "2"}).Updates(map[string]interface{}{"status": status, "error_message": failure, "rsp_data": string(msg.Payload)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		var terminal struct{ Status string }
		q := tx.WithContext(ctx).Table(table).Select("status").Where("device_id=? AND message_id=?", logicalDevice, id).Take(&terminal)
		if q.Error == nil && terminal.Status == status {
			return nil
		}
		return errors.New("response does not match exactly one scoped operation")
	}
	return nil
}
