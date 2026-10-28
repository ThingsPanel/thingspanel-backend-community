package mqttadapter

import (
	"errors"
	"gorm.io/gorm"
	"project/internal/brokercommands"
	bc "project/internal/brokercontract"
	"project/internal/dal"
	"project/pkg/global"
	"strings"
)

func (a *Adapter) commandResponseScope(tenant, transport, id string, epoch uint64) (string, error) {
	row, err := brokercommands.New(global.DB).GetScoped(a.adapterContext(), tenant, id)
	if err == nil {
		if ((strings.HasPrefix(row.Intent, "expected.") && row.Intent != "expected.command") || strings.HasPrefix(row.Intent, "control.")) || row.TargetDeviceID != transport || row.RouteEpoch <= 0 || uint64(row.RouteEpoch) != epoch {
			return "", bc.ErrUnauthorized
		}
		return row.DeviceID, nil
	}
	if !errors.Is(err, brokercommands.ErrNotFound) {
		return "", err
	}
	// In-flight legacy commands can finish after cutover, still scoped to the
	// authenticated device. No message_id-only or gateway guessing fallback.
	log, err := dal.GetCommandSetLogByMessageID(id, transport)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	if err != nil || log == nil || log.DeviceID != transport {
		return "", bc.ErrUnauthorized
	}
	return transport, nil
}
