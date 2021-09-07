package config

func NewCoordinateConfig() *CoordinateConfig {
	return &CoordinateConfig{}
}

type CoordinateConfig struct {
	From string `json:"from" env:"COORDINATE_FROM" envDefault:"WGS-84"` //GPS原始坐标
}
