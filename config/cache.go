package config

func NewCacheConfig() *CacheConfig {
	cc := &CacheConfig{}
	cc.Stores.APC.Driver = `apc`
	cc.Stores.Array.Driver = `array`
	cc.Stores.Database.Driver = `database`
	cc.Stores.Database.Table = `cache`
	cc.Stores.Database.Connection = nil
	cc.Stores.File.Driver = `file`
	cc.Stores.File.Path = `framework/cache/data`
	cc.Stores.MemCached.Driver = `memcached`
	cc.Stores.MemCached.Options.ConnectionTimeOut = 2000
	cc.Stores.MemCached.Servers.Weight = 100
	cc.Stores.Redis.Driver = `redis`
	cc.Stores.Redis.Connection = `cache`
	cc.Stores.DynamoDB.Driver = `dynamodb`

	return cc
}

type CacheConfig struct {
	/*
	   |--------------------------------------------------------------------------
	   | Default Cache Store
	   |--------------------------------------------------------------------------
	   |
	   | This option controls the default cache connection that gets used while
	   | using this caching library. This connection is used when another is
	   | not explicitly specified when executing a given caching function.
	   |
	   | Supported: "apc", "array", "database", "file",
	   |            "memcached", "redis", "dynamodb"
	   |
	*/
	Default string `json:"default" env:"CACHE_DRIVER" envDefault:"file"`
	/*
	   |--------------------------------------------------------------------------
	   | Cache Stores
	   |--------------------------------------------------------------------------
	   |
	   | Here you may define all of the cache "stores" for your application as
	   | well as their drivers. You may even define multiple stores for the
	   | same cache driver to group types of items stored in your caches.
	   |
	*/
	Stores struct {
		APC struct {
			Driver string `json:"driver"`
		} `json:"apc"`
		Array struct {
			Driver string `json:"driver"`
		} `json:"array"`
		Database struct {
			Driver     string  `json:"driver"`
			Table      string  `json:"cache"`
			Connection *string `json:"connection"`
		} `json:"database"`
		File struct {
			Driver string `json:"driver"`
			Path   string `json:"path"`
		} `json:"file"`
		MemCached struct {
			Driver       string `json:"driver"`
			PersistentID string `json:"persistent_id" env:"MEMCACHED_PERSISTENT_ID"`
			SASL         struct {
				User     string `json:"user" env:"MEMCACHED_USERNAME"`
				Password string `json:"password" env:"MEMCACHED_PASSWORD"`
			} `json:"sasl"`
			Options struct {
				ConnectionTimeOut int64 `json:"connection_time_out"`
			} `json:"options"`
			Servers struct {
				Host   string `json:"host" env:"MEMCACHED_HOST" envDefault:"127.0.0.1"`
				Port   int    `json:"port" env:"MEMCACHED_PORT" envDefault:"11211"`
				Weight int    `json:"weight"`
			}
		} `json:"mem_cached"`
		Redis struct {
			Driver     string `json:"driver"`
			Connection string `json:"connection"`
		}

		DynamoDB struct {
			Driver   string `json:"driver"`
			Key      string `json:"key" env:"AWS_ACCESS_KEY_ID"`
			Secret   string `json:"secret" env:"AWS_SECRET_ACCESS_KEY"`
			Region   string `json:"region" env:"AWS_DEFAULT_REGION" envDefault:"us-ease-1"`
			Table    string `json:"table" env:"DYNAMODB_CACHE_TABLE" envDefault:"cache"`
			Endpoint string `json:"endpoint" env:"DYNAMODB_ENDPOINT"`
		}
	}
	/*
	   |--------------------------------------------------------------------------
	   | Cache Key Prefix
	   |--------------------------------------------------------------------------
	   |
	   | When utilizing a RAM based store such as APC or Memcached, there might
	   | be other applications utilizing the same cache. So, we'll specify a
	   | value to get prefixed to all our keys so we can avoid collisions.
	   |
	*/
	Prefix string `json:"prefix" env:"CACHE_PREFIX" envDefault:""` //todo-initialize later on
}
