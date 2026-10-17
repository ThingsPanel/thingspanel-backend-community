package service

import (
	"encoding/json"
	"strings"
	"testing"

	"project/internal/model"
)

func TestDeviceListProductKeyComesFromRequiredProductAssociation(t *testing.T) {
	item := model.GetDeviceListByPageRsp{DeviceConfigName: "A100 电子吧唧模板 01", ProductID: "product-a100", ProductCode: "A100"}
	key, err := requiredProductCode(item)
	if err != nil {
		t.Fatal(err)
	}
	item.YgsoulProductKey = key
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"ygsoulProductKey":"A100"`) {
		t.Fatalf("required product key missing from list response: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"productCode":"A100"`) || !strings.Contains(string(encoded), `"product_id":"product-a100"`) {
		t.Fatalf("required product association missing from list response: %s", encoded)
	}
	if _, err := requiredProductCode(model.GetDeviceListByPageRsp{DeviceConfigName: "A100 电子吧唧模板 01"}); err == nil {
		t.Fatal("missing required product association must be rejected")
	}
}

func TestPreRegisterRequiresProductCodeAndDeviceConfig(t *testing.T) {
	code := "A100"
	config := "mqtt-config"
	if err := validatePreRegisterProduct(model.Product{ProductModel: &code, DeviceConfigID: &config}); err != nil {
		t.Fatal(err)
	}
	if err := validatePreRegisterProduct(model.Product{ProductModel: &code}); err == nil {
		t.Fatal("product without device config must be rejected")
	}
	if err := validatePreRegisterProduct(model.Product{DeviceConfigID: &config}); err == nil {
		t.Fatal("product without model code must be rejected")
	}
}
