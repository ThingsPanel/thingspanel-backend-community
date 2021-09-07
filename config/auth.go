package config

func NewAuthConfig() *AuthConfig {
	ac := AuthConfig{}
	ac.Defaults.Guard = `api`
	ac.Defaults.Passwords = `users`
	ac.Passwords = make(map[string]PasswordResetSetting)
	ac.Passwords[`users`] = PasswordResetSetting{
		Table:    "password_resets",
		Expire:   60,
		Throttle: 60,
	}
	ac.PassWordTimeout = 10800
	return &ac
}

type AuthConfig struct {
	/*
	   |--------------------------------------------------------------------------
	   | Defaults
	   |--------------------------------------------------------------------------
	   |
	   | This option controls the default authentication "guard" and password
	   | reset options for your application. You may change these defaults
	   | as required, but they're a perfect start for most applications.
	   |
	*/
	Defaults struct {
		Guard     string `json:"guard"`
		Passwords string `json:"passwords"`
	}
	Passwords map[string]PasswordResetSetting `json:"passwords"`
	/*
	   |--------------------------------------------------------------------------
	   | Password Confirmation Timeout
	   |--------------------------------------------------------------------------
	   |
	   | Here you may define the amount of seconds before a password confirmation
	   | times out and the user is prompted to re-enter their password via the
	   | confirmation screen. By default, the timeout lasts for three hours.
	   |
	*/
	PassWordTimeout int64 `json:"password_timeout"`
}

type Guard struct {
	Driver   string `json:"driver"`
	Provider string `json:"provider"`
	Hash     string `json:"hash"`
}

type PasswordResetSetting struct {
	Table    string `json:"table"`
	Expire   int    `json:"expire"`
	Throttle int    `json:"throttle"`
}
