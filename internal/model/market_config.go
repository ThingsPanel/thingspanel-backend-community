package model

// MarketConfig holds configuration for the Horizon Market Service integration
type MarketConfig struct {
	Enabled bool   `yaml:"enabled"`
	BaseURL string `yaml:"base_url"`
}
