package config

func NewBroadCasterConfig() *BroadCasterConfig {
	bc := &BroadCasterConfig{}
	bc.Connections.Pusher.Driver = `pusher`
	bc.Connections.Pusher.Options.UseTLS = true
	bc.Connections.Redis.Driver = `redis`
	bc.Connections.Redis.Connection = `default`
	bc.Connections.Log.Driver = `log`

	return bc
}

type BroadCasterConfig struct {
	/*
	   |--------------------------------------------------------------------------
	   | Default Broadcaster
	   |--------------------------------------------------------------------------
	   |
	   | This option controls the default broadcaster that will be used by the
	   | framework when an event needs to be broadcast. You may set this to
	   | any of the connections defined in the "connections" array below.
	   |
	   | Supported: "pusher", "redis", "log", "null"
	   |
	*/
	Default *string `json:"default" env:"BROADCAST_DRIVER",envDefault:"null"`

	/*
	   |--------------------------------------------------------------------------
	   | Broadcast Connections
	   |--------------------------------------------------------------------------
	   |
	   | Here you may define all of the broadcast connections that will be used
	   | to broadcast events to other systems or over websockets. Samples of
	   | each available type of connection are provided inside this array.
	   |
	*/
	Connections struct {
		Pusher struct {
			Driver  string `json:"driver"`
			Key     string `json:"key" env:"PUSHER_APP_KEY"`
			Secret  string `json:"secret" env:"PUSHER_APP_SECRET"`
			AppID   string `json:"app_id" env:"PUSHER_APP_ID"`
			Options struct {
				Cluster string `json:"cluster" env:"PUSHER_APP_CLUSTER"`
				UseTLS  bool   `json:"useTLS"`
			}
		}
		Redis struct {
			Driver     string `json:"driver"`
			Connection string `json:"connection"`
		}
		Log struct {
			Driver string `json:"driver"`
		}
	}
}
