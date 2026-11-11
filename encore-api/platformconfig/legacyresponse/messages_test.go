package legacyresponse

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDictionaryResponseUsesExistingMessageAsset(t *testing.T) {
	catalog, err := Load(filepath.Join("..", "..", "..", "configs", "messages.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ header, want string }{
		{"", "操作成功"}, {"en-US", "Success"}, {"fr-FR,en-US;q=0.7", "Success"},
		{"en-US;q=0.3,zh-CN;q=0.9", "操作成功"}, {"en", "操作成功"},
		{"en-US;q=bad,zh-CN;q=0.1", "Success"},
	} {
		if actual := catalog.Message(200, test.header); actual != test.want {
			t.Fatalf("header=%q actual=%q expected=%q", test.header, actual, test.want)
		}
	}
	if actual := catalog.Message(101001, "en-US"); actual != "Database Connection Failed" {
		t.Fatalf("existing DB message changed: %q", actual)
	}
	if actual := catalog.Message(987654, "en-US"); actual != "未知错误" {
		t.Fatalf("unknown-code fallback changed: %q", actual)
	}
	file := filepath.Join(t.TempDir(), "incomplete.yaml")
	if err := os.WriteFile(file, []byte("messages:\n  200:\n    zh_CN: success\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Fatal("accepted incomplete message asset")
	}
}
