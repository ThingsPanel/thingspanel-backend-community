package service

import (
	"testing"

	"project/internal/model"
)

func TestProductTOSImageURLStoredSeparately(t *testing.T) {
	info := `{"existing":"kept"}`
	url := "https://yomipro1.tos-cn-beijing.volces.com/product-assets/v1/a100.png"
	merged := mergeTOSImageURL(&info, &url)
	restored := readTOSImageURL(merged)
	if restored == nil {
		t.Fatal("missing tos_image_url")
	}
	if *restored != url {
		t.Fatalf("got %q, want %q", *restored, url)
	}
}

func TestHydrateProductTOSImageURLsUpdatesReturnedList(t *testing.T) {
	info := `{"tos_image_url":"https://yomipro1.tos-cn-beijing.volces.com/product-assets/v1/a100.png"}`
	list := []model.ProductList{{Product: model.Product{AdditionalInfo: &info}}}

	hydrateProductTOSImageURLs(list)

	if list[0].TOSImageURL == nil || *list[0].TOSImageURL == "" {
		t.Fatal("product list did not expose tos_image_url")
	}
}
