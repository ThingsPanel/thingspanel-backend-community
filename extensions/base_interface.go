package extensions

type BaseInterface interface {
	Main(device_id []string, data []string, fields []string, initial bool) []string
}
