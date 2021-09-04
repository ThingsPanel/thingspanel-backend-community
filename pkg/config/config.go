package config

// config package generalizes the application configurations

type Config interface {
	Print()
	Validate() error
	Init() error
	Name() string
}
