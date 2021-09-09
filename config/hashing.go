package config

func NewHashingConfig() *HashingConfig {
	hc := &HashingConfig{}
	hc.Driver = `bcrypt`
	hc.Argon.Memory = 1024
	hc.Argon.Threads = 2
	hc.Argon.Time = 2

	return hc
}

type HashingConfig struct {
	/*
	   |--------------------------------------------------------------------------
	   | Default Hash Driver
	   |--------------------------------------------------------------------------
	   |
	   | This option controls the default hash driver that will be used to hash
	   | passwords for your application. By default, the bcrypt algorithm is
	   | used; however, you remain free to modify this option if you wish.
	   |
	   | Supported: "bcrypt", "argon", "argon2id"
	   |
	*/
	Driver string `json:"driver"`
	/*
	   |--------------------------------------------------------------------------
	   | Bcrypt Options
	   |--------------------------------------------------------------------------
	   |
	   | Here you may specify the configuration options that should be used when
	   | passwords are hashed using the Bcrypt algorithm. This will allow you
	   | to control the amount of time it takes to hash the given password.
	   |
	*/
	Bcrypt struct {
		Rounds int `json:"rounds" env:"BCRYPT_ROUNDS" envDefault:"10"`
	} `json:"bcrypt"`
	/*
	   |--------------------------------------------------------------------------
	   | Argon Options
	   |--------------------------------------------------------------------------
	   |
	   | Here you may specify the configuration options that should be used when
	   | passwords are hashed using the Argon algorithm. These will allow you
	   | to control the amount of time it takes to hash the given password.
	   |
	*/
	Argon struct {
		Memory  int `json:"memory"`
		Threads int `json:"threads"`
		Time    int `json:"time"`
	} `json:"argon"`
}
