package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"encore.app/notification-adapters/sdk"
	mailadapter "encore.app/notification-adapters/smtp"
)

func main() {
	token := os.Getenv("NOTIFICATION_PLUGIN_TOKEN")
	if token == "" {
		log.Fatal("NOTIFICATION_PLUGIN_TOKEN is required")
	}
	servers := splitEnv("NOTIFICATION_SMTP_ALLOWED_SERVERS")
	cidrs := splitEnv("NOTIFICATION_SMTP_ALLOWED_CIDRS")
	provider, err := mailadapter.New(servers, cidrs)
	if err != nil {
		log.Fatal("invalid SMTP allowlist configuration")
	}
	handler, err := sdk.NewHandler(token, provider, sdk.HandlerOptions{AllowedUIOrigins: splitEnv("NOTIFICATION_PLUGIN_UI_ORIGINS")})
	if err != nil {
		log.Fatal("invalid plugin server configuration")
	}
	address := os.Getenv("NOTIFICATION_PLUGIN_LISTEN")
	if address == "" {
		address = "127.0.0.1:5005"
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("plugin server stopped")
	}
}
func splitEnv(key string) []string {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(value, ",") {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return out
}
