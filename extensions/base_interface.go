package extensions

type BaseInterface interface {
	Main(device_ids []string, startTs int64, endTs int64) []interface{}
}
