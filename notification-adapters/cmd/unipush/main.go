package main

import (
	"encore.app/notification-adapters/sdk"
	"encore.app/notification-adapters/unipush"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	var provider sdk.Plugin
	if os.Getenv("NOTIFICATION_ALLOW_UNCONFIGURED") == "true" && os.Getenv("NOTIFICATION_UNIPUSH_ENDPOINT") == "" && os.Getenv("NOTIFICATION_UNIPUSH_CLOUD_TOKEN") == "" {
		provider = sdk.Unconfigured{Definition: (&unipush.Provider{}).Manifest()}
	} else {
		p, e := unipush.New(os.Getenv("NOTIFICATION_UNIPUSH_ENDPOINT"), os.Getenv("NOTIFICATION_UNIPUSH_ALLOW_HTTP") == "1")
		if e != nil {
			log.Fatal(e)
		}
		if e = p.ConfigureDirectory(os.Getenv("NOTIFICATION_UNIPUSH_DIRECTORY_URL"), os.Getenv("NOTIFICATION_UNIPUSH_DIRECTORY_TOKEN"), os.Getenv("NOTIFICATION_UNIPUSH_DEPLOYMENT_ID"), os.Getenv("NOTIFICATION_UNIPUSH_APP_ID"), os.Getenv("NOTIFICATION_UNIPUSH_CLOUD_TOKEN"), os.Getenv("NOTIFICATION_UNIPUSH_ALLOW_HTTP") == "1"); e != nil {
			log.Fatal(e)
		}
		provider = p
	}
	var origins []string
	if v := os.Getenv("NOTIFICATION_PLUGIN_UI_ORIGINS"); v != "" {
		origins = strings.Split(v, ",")
	}
	h, e := sdk.NewHandler(os.Getenv("NOTIFICATION_PLUGIN_TOKEN"), provider, sdk.HandlerOptions{AllowedUIOrigins: origins})
	if e != nil {
		log.Fatal("invalid plugin configuration")
	}
	initial := sdk.DeploymentSettings{}
	if raw := os.Getenv("NOTIFICATION_UNIPUSH_ENDPOINT"); raw != "" {
		if u, err := url.Parse(raw); err == nil && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
			initial.Address = raw
			initial.Secret = os.Getenv("NOTIFICATION_UNIPUSH_CLOUD_TOKEN")
		}
	}
	h, e = sdk.WithDeploymentSettings(h, os.Getenv("NOTIFICATION_PLUGIN_TOKEN"), os.Getenv("NOTIFICATION_SETTINGS_FILE"), initial, func(settings sdk.DeploymentSettings) (http.Handler, error) {
		p, err := unipush.New(settings.Address, false)
		if err != nil {
			return nil, err
		}
		if settings.Secret == "" {
			return nil, errors.New("missing cloud token")
		}
		err = p.ConfigureDirectory(os.Getenv("NOTIFICATION_UNIPUSH_DIRECTORY_URL"), os.Getenv("NOTIFICATION_UNIPUSH_DIRECTORY_TOKEN"), os.Getenv("NOTIFICATION_UNIPUSH_DEPLOYMENT_ID"), os.Getenv("NOTIFICATION_UNIPUSH_APP_ID"), settings.Secret, os.Getenv("NOTIFICATION_UNIPUSH_ALLOW_HTTP") == "1")
		if err != nil {
			return nil, err
		}
		if err = p.RestrictCloudEndpoint(); err != nil {
			return nil, err
		}
		return sdk.NewHandler(os.Getenv("NOTIFICATION_PLUGIN_TOKEN"), p, sdk.HandlerOptions{AllowedUIOrigins: origins})
	})
	if e != nil {
		log.Fatal("cannot load App push settings")
	}
	address := os.Getenv("NOTIFICATION_PLUGIN_LISTEN")
	if address == "" {
		address = "127.0.0.1:5009"
	}
	log.Fatal(sdk.Serve(&http.Server{Addr: address, Handler: h, ReadHeaderTimeout: 5 * time.Second}))
}
