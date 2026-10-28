package notification

import (
	"encore.app/l3-core/notification/contract"
	"strings"
	"testing"
)

func TestHistoryCompatibilityNeverClaimsProviderAcceptanceIsDelivery(t *testing.T) {
	item := legacyHistoryItem(contract.NotificationView{ID: "native", IntakeStatus: "ready", Deliveries: []contract.DeliverySummary{{DispatchStatus: "accepted", DeliveryStatus: "unsupported"}}})
	if item.RuleID != "" || item.DeviceID != "" || !strings.Contains(item.Message, "dispatch=accepted,delivery=unsupported") || strings.Contains(item.Message, "delivered") {
		t.Fatal("compatibility invented device/rule/delivery evidence")
	}
}
