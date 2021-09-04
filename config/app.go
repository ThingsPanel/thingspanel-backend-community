package config

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/pkg/env"
	"github.com/go-playground/validator"
)

func NewAppConfig() *AppConfig {
	return &AppConfig{}
}

type AppConfig struct {
	/*
	   |--------------------------------------------------------------------------
	   | AppName
	   |--------------------------------------------------------------------------
	   |
	   | This value is the name of your application. This value is used when the
	   | api needs to place the application's name in a notification or
	   | any other location as required by the application or its packages.
	   | Default: ThingsPanel-Go
	*/
	AppName string `json:"app_name" env:"APP_NAME" envDefault:"ThingsPanel-Go" validate:"required"`
	/*
	   |--------------------------------------------------------------------------
	   | Environment
	   |--------------------------------------------------------------------------
	   |
	   | This value determines the "environment" your application is currently
	   | running in. This may determine how you prefer to configure various
	   | services the application utilizes. Set this in your ".env" file.
	   | Default: production
	*/
	Environment string `json:"environment" env:"APP_ENV" envDefault:"production" validate:"required"`
	/*
	   |--------------------------------------------------------------------------
	   | Debug
	   |--------------------------------------------------------------------------
	   |
	   | When your application is in debug mode, detailed error messages with
	   | stack traces will be shown on every error that occurs within your
	   | application. If disabled, a simple generic error page is shown.
	   | Default: false
	*/
	Debug bool `json:"debug" env:"APP_DEBUG"`
	/*
	   |--------------------------------------------------------------------------
	   | AppURL
	   |--------------------------------------------------------------------------
	   |
	   | This URL is used by the console to properly generate URLs when using
	   | the command line tool. You should set this to the root of
	   | your application.
	   | Default: "http://localhost"
	*/
	AppURL   string  `json:"app_url" env:"APP_URL" envDefault:"http://localhost" validate:"required"`
	AssetURL *string `env:"ASSET_URL"`
	/*
	   |--------------------------------------------------------------------------
	   | TimeZone
	   |--------------------------------------------------------------------------
	   |
	   | Here you may specify the default timezone for your application, which
	   | will be used by the Go date and date-time functions. We have gone
	   | ahead and set this to a sensible default for you out of the box.
	   | Default: 'Asia/Shanghai'
	*/
	TimeZone string `json:"time_zone" env:"TIME_ZONE" envDefault:"Asia/Shanghai" validate:"required"`
	/*
	   |--------------------------------------------------------------------------
	   | Locale
	   |--------------------------------------------------------------------------
	   |
	   | The application locale determines the default locale that will be used
	   | by the translation service provider. You are free to set this value
	   | to any of the locales which will be supported by the application.
	   | Default: 'en''
	*/
	Locale string `json:"locale" env:"LOCALE" envDefault:"en" validate:"required"`
	/*
	   |--------------------------------------------------------------------------
	   | FallbackLocale
	   |--------------------------------------------------------------------------
	   |
	   | The fallback locale determines the locale to use when the current one
	   | is not available. You may change the value to correspond to any of
	   | the language folders that are provided through your application.
	   |
	*/
	FallbackLocale string `json:"fallback_locale" env:"FALLBACK_LOCALE" envDefault:"en" validate:"required"`
	// todo - include faker locale?
	/*
	   |--------------------------------------------------------------------------
	   | Key
	   |--------------------------------------------------------------------------
	   |
	   | This key is used by the Illuminate encrypter service and should be set
	   | to a random, 32 character string, otherwise these encrypted strings
	   | will not be safe. Please do this before deploying an application!
	   |
	*/
	Key    string `json:"key" env:"APP_KEY" validate:"required,len=32"`
	Cipher string `json:"cipher" validate:"required"`
}

func (a *AppConfig) Print() string {
	byt, _ := json.Marshal(*a)
	return string(byt)
}

func (a *AppConfig) Validate() error {
	v := validator.New()
	return v.Struct(*a)
}

func (a *AppConfig) Init() error {
	return env.Parse(a)
}
