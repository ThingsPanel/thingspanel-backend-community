package services

import (
	"ThingsPanel-Go/services"
	"fmt"
	"testing"
)

var TSKVService services.TSKVService

func TestGetTelemetry(t *testing.T) {
	device_ids := []string{"7d61d336-c800-7e74-b54b-dace04507166"}
	warningLogs := TSKVService.GetTelemetry(device_ids, 1644288241274, 1644888241274, "")
	fmt.Println(warningLogs)

}
