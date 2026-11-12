package main

import (
	"fmt"

	"project/internal/app"
	"project/internal/automationbridge"
)

// runConfigCheck validates what a restart would depend on, without touching
// any external service: the configuration file or production config, the
// environment the automation bridge needs, and the RSA key. It never connects
// to PostgreSQL, Redis or the broker, so it is safe to run while they are down.
// Errors name the failing item but never print configuration values.
func runConfigCheck(configPath string) error {
	if err := automationbridge.Initialize(); err != nil {
		return fmt.Errorf("automation configuration invalid: %w", err)
	}
	var load app.Option
	if configPath != "" {
		load = app.WithConfigFile(configPath)
	} else {
		load = app.WithProductionConfig()
	}
	probe := &app.Application{}
	if err := load(probe); err != nil {
		return fmt.Errorf("configuration cannot be loaded: %w", err)
	}
	if err := app.WithRsaDecrypt("./configs/rsa_key/private_key.pem")(probe); err != nil {
		return fmt.Errorf("RSA key cannot be loaded: %w", err)
	}
	return nil
}
