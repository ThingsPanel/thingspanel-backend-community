//go:build encore_app

package notification

import (
	"encore.dev/appruntime/shared/encoreenv"
	"os"
)

// Encore consumes and removes its startup variables. The isolated crash-test
// child uses the same compiled test binary and needs that runtime configuration.
// Pass only the runtime's own configuration, without logging any of its values.
func workerChildEnvironment() []string {
	environment := os.Environ()
	for _, key := range []string{"ENCORE_RUNTIME_CONFIG", "ENCORE_RUNTIME_CONFIG_PATH", "ENCORE_PROCESS_CONFIG", "ENCORE_INFRA_CONFIG_PATH", "ENCORE_DEPLOY_ID", "ENCORE_APP_SECRETS", "ENCORE_SECRET_PROVIDERS"} {
		if value := encoreenv.Get(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return environment
}
