package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUploadTypePrefersExplicitQuery(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/v1/file/up?type=product-image", nil)
	if got := resolveUploadType(c); got != "product-image" {
		t.Fatalf("resolveUploadType() = %q", got)
	}
}
