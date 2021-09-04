package config

// config package generalizes the application configurations

type Config interface {
	Print() string
	Validate() error
	Init() error
}
