package dal

import (
	"project/internal/model"
	"project/internal/query"
)

// HistoryBackend is the single entry for reading, exporting, aggregating and
// deleting telemetry history. The default implementation is the existing
// PostgreSQL/gRPC code in this package, unchanged; another time-series backend
// replaces it through SetHistoryBackend rather than by editing every caller.
// Writing history is not here: it goes through storage.TelemetryHistoryStore.
type HistoryBackend interface {
	GetHistoryTelemetrData(deviceId, key string, startTime, endTime int64) ([]*model.TelemetryData, error)
	GetHistoryTelemetrDataByPage(p *model.GetTelemetryHistoryDataByPageReq) (int64, []*model.TelemetryData, error)
	GetHistoryTelemetrDataByExport(p *model.GetTelemetryHistoryDataByPageReq, offset, batchSize int) ([]*model.TelemetryData, error)
	GetTelemetrStatisticData(deviceID, key string, startTime, endTime int64) ([]map[string]interface{}, error)
	GetTelemetrStatisticaAgregationData(deviceId, key string, sTime, eTime, aggregateWindow int64, aggregateFunc string) ([]map[string]interface{}, error)
	GetTelemetryStatisticDataByDeviceIds(deviceIds []string, keys []string, timeType string, limit *int, aggregateMethod string) ([]map[string]interface{}, error)
	GetTelemetryDataCountByTenantId(tenantId string) (int64, error)
	DeleteTelemetrData(deviceId, key string) error
	DeleteTelemetrDataByTime(t int64) error
	// DeleteTelemetrDataByDeviceId runs inside the device-deletion transaction; a
	// backend outside that transaction must make the removal durable and replayable.
	DeleteTelemetrDataByDeviceId(deviceId string, tx *query.QueryTx) error
}

type postgresHistoryBackend struct{}

func (postgresHistoryBackend) GetHistoryTelemetrData(deviceId, key string, startTime, endTime int64) ([]*model.TelemetryData, error) {
	return GetHistoryTelemetrData(deviceId, key, startTime, endTime)
}
func (postgresHistoryBackend) GetHistoryTelemetrDataByPage(p *model.GetTelemetryHistoryDataByPageReq) (int64, []*model.TelemetryData, error) {
	return GetHistoryTelemetrDataByPage(p)
}
func (postgresHistoryBackend) GetHistoryTelemetrDataByExport(p *model.GetTelemetryHistoryDataByPageReq, offset, batchSize int) ([]*model.TelemetryData, error) {
	return GetHistoryTelemetrDataByExport(p, offset, batchSize)
}
func (postgresHistoryBackend) GetTelemetrStatisticData(deviceID, key string, startTime, endTime int64) ([]map[string]interface{}, error) {
	return GetTelemetrStatisticData(deviceID, key, startTime, endTime)
}
func (postgresHistoryBackend) GetTelemetrStatisticaAgregationData(deviceId, key string, sTime, eTime, aggregateWindow int64, aggregateFunc string) ([]map[string]interface{}, error) {
	return GetTelemetrStatisticaAgregationData(deviceId, key, sTime, eTime, aggregateWindow, aggregateFunc)
}
func (postgresHistoryBackend) GetTelemetryStatisticDataByDeviceIds(deviceIds []string, keys []string, timeType string, limit *int, aggregateMethod string) ([]map[string]interface{}, error) {
	return GetTelemetryStatisticDataByDeviceIds(deviceIds, keys, timeType, limit, aggregateMethod)
}
func (postgresHistoryBackend) GetTelemetryDataCountByTenantId(tenantId string) (int64, error) {
	return GetTelemetryDataCountByTenantId(tenantId)
}
func (postgresHistoryBackend) DeleteTelemetrData(deviceId, key string) error {
	return DeleteTelemetrData(deviceId, key)
}
func (postgresHistoryBackend) DeleteTelemetrDataByTime(t int64) error {
	return DeleteTelemetrDataByTime(t)
}
func (postgresHistoryBackend) DeleteTelemetrDataByDeviceId(deviceId string, tx *query.QueryTx) error {
	return DeleteTelemetrDataByDeviceId(deviceId, tx)
}

// History is the active backend. Set it once at startup, before serving requests.
var History HistoryBackend = postgresHistoryBackend{}

// SetHistoryBackend installs a backend and returns a function restoring the previous one.
func SetHistoryBackend(b HistoryBackend) (restore func()) {
	previous := History
	History = b
	return func() { History = previous }
}
