package main

import (
	"encore.app/notification-adapters/sdk"
	"encore.app/notification-adapters/unipush"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	p, e := unipush.New(os.Getenv("NOTIFICATION_UNIPUSH_ENDPOINT"), os.Getenv("NOTIFICATION_UNIPUSH_ALLOW_HTTP") == "1")
	if e != nil {
		log.Fatal(e)
	}
	if e = p.ConfigureDirectory(os.Getenv("NOTIFICATION_UNIPUSH_DIRECTORY_URL"), os.Getenv("NOTIFICATION_UNIPUSH_DIRECTORY_TOKEN"), os.Getenv("NOTIFICATION_UNIPUSH_DEPLOYMENT_ID"), os.Getenv("NOTIFICATION_UNIPUSH_APP_ID"), os.Getenv("NOTIFICATION_UNIPUSH_CLOUD_TOKEN"), os.Getenv("NOTIFICATION_UNIPUSH_ALLOW_HTTP") == "1"); e != nil {
		log.Fatal(e)
	}
	var origins []string
	if v := os.Getenv("NOTIFICATION_PLUGIN_UI_ORIGINS"); v != "" {
		origins = strings.Split(v, ",")
	}
	h, e := sdk.NewHandler(os.Getenv("NOTIFICATION_PLUGIN_TOKEN"), p, sdk.HandlerOptions{AllowedUIOrigins: origins})
	if e != nil {
		log.Fatal("invalid plugin configuration")
	}
	address := os.Getenv("NOTIFICATION_PLUGIN_LISTEN")
	if address == "" {
		address = "127.0.0.1:5009"
	}
	log.Fatal((&http.Server{Addr: address, Handler: h, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}
