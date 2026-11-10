package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"plugin.local/example-notification/example"
	"plugin.local/example-notification/sdk"
)

func main() {
	token := os.Getenv("NOTIFICATION_PLUGIN_TOKEN")
	if token == "" {
		log.Fatal("NOTIFICATION_PLUGIN_TOKEN is required")
	}
	listen := os.Getenv("NOTIFICATION_PLUGIN_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:5080"
	}
	if !strings.HasPrefix(listen, "127.0.0.1:") && !strings.HasPrefix(listen, "[::1]:") {
		log.Fatal("the example binds to loopback only")
	}
	handler, err := sdk.NewHandler(token, example.Provider{}, sdk.HandlerOptions{})
	if err != nil {
		log.Fatal("could not initialize the example plugin")
	}
	server := &http.Server{Addr: listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	log.Print("local-only plugin example listening on loopback")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("local example server stopped")
	}
}
