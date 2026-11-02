package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"project/pkg/global"
)

func TestDictionarySelectedNativeOwnerClosesEveryLegacyEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := global.DB
	global.DB = nil
	t.Cleanup(func() { global.DB = previous })
	api := new(DictApi)
	for _, route := range []struct {
		method, path string
		handler      gin.HandlerFunc
	}{
		{http.MethodPost, "/api/v1/dict/column", api.CreateDictColumn},
		{http.MethodPost, "/api/v1/dict/language", api.CreateDictLanguage},
		{http.MethodGet, "/api/v1/dict/enum", api.HandleDict},
		{http.MethodGet, "/api/v1/dict", api.HandleDictLisyByPage},
		{http.MethodGet, "/api/v1/dict/language/:id", api.HandleDictLanguage},
		{http.MethodDelete, "/api/v1/dict/column/:id", api.DeleteDictColumn},
		{http.MethodDelete, "/api/v1/dict/language/:id", api.DeleteDictLanguage},
		{http.MethodGet, "/api/v1/dict/protocol/service", api.HandleProtocolAndService},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			for _, owner := range []string{"encore", "invalid-owner"} {
				t.Setenv("TP_PLATFORMCONFIG_OWNER", owner)
				router := gin.New()
				router.Handle(route.method, route.path, route.handler)
				request := httptest.NewRequest(route.method, strings.ReplaceAll(route.path, ":id", "old-opaque-id"), strings.NewReader(`{"dict_code":"must-not-write","dict_value":"value"}`))
				w := httptest.NewRecorder()
				router.ServeHTTP(w, request)
				var envelope struct {
					Code int `json:"code"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || w.Code != 503 || envelope.Code != 100001 {
					t.Fatalf("legacy adapter owner=%q status=%d body=%s", owner, w.Code, w.Body.String())
				}
			}
		})
	}
}

// The default leaves the established handler body intact. This unit check does
// not claim that the old DAL itself has acquired native transaction semantics.
func TestDictionaryDefaultOwnerPreservesLegacyRouting(t *testing.T) {
	for _, owner := range []string{"", "disabled"} {
		t.Setenv("TP_PLATFORMCONFIG_OWNER", owner)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		if !dictionaryLegacyAvailable(c) || c.IsAborted() || c.Writer.Written() {
			t.Fatal("disabled native owner intercepted established routing")
		}
	}
}
