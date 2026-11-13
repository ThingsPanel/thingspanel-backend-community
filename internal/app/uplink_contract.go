package app

import (
	"os"
	"project/internal/automationbridge"
	"project/internal/uplinkcontract"
	"strings"
)

// One bounded immutable cache is shared by telemetry and reliable ingress.
// Missing owner configuration preserves unbound-device compatibility while
// native-bound messages fail closed at their model validation boundary.
func (a *Application) uplinkContractClient() *uplinkcontract.Client {
	a.uplinkContractsOnce.Do(func() {
		client, err := uplinkcontract.NewClient(uplinkcontract.Config{BaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("TP_THINGMODEL_NATIVE_URL")), "/"), Token: strings.TrimSpace(os.Getenv("GOTP_AUTOMATION_TOKEN")), Deployment: automationbridge.Deployment()})
		if err != nil {
			if a.Logger != nil {
				a.Logger.Warn("native uplink contract owner unavailable; bound messages will be rejected")
			}
			return
		}
		a.uplinkContracts = client
	})
	return a.uplinkContracts
}
