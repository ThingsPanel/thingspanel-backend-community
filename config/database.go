package config

func NewDatabaseConfig() *DatabaseConfig {
	dc := &DatabaseConfig{}
	dc.Connection.SQLite.Driver = `sqlite`
	dc.Connection.SQLite.Prefix = ""
	dc.Connection.MySQL.Driver = `mysql`
	dc.Connection.MySQL.Charset = `utf8mb4`
	dc.Connection.MySQL.Collation = `utf8mb4_unicode_ci`
	dc.Connection.MySQL.Prefix = ""
	dc.Connection.MySQL.PrefixIndexes = true
	dc.Connection.MySQL.Strict = true
	dc.Connection.MySQL.Engine = nil

	dc.Connection.PgSQL.Driver = `pgsql`
	dc.Connection.PgSQL.Charset = `utf8`
	dc.Connection.PgSQL.Prefix = ""
	dc.Connection.PgSQL.PrefixIndexes = true
	dc.Connection.PgSQL.Schema = `public`
	dc.Connection.PgSQL.SSLMode = `prefer`

	dc.Connection.SQLServer.Driver = `pgsql`
	dc.Connection.SQLServer.Charset = `utf8`
	dc.Connection.SQLServer.Prefix = ""
	dc.Connection.SQLServer.PrefixIndexes = true

	dc.Migrations = `migrations`

	return dc
}

type DatabaseConfig struct {
	/*
	   |--------------------------------------------------------------------------
	   | Default Database Connection Name
	   |--------------------------------------------------------------------------
	   |
	   | Here you may specify which of the database connections below you wish
	   | to use as your default connection for all database work. Of course
	   | you may use many connections at once using the Database library.
	   |
	*/
	Default string `json:"default" env:"DB_CONNECTION" envDefault:"mysql"`
	/*
	   |--------------------------------------------------------------------------
	   | Database Connections
	   |--------------------------------------------------------------------------
	   |
	   | Here are each of the database connections setup for your application.
	   | Of course, examples of configuring each database platform that is
	   | supported by Go is shown below to make development simple.
	   |
	   |
	   | All database work in Gorm.
	   | so make sure you have the driver for your particular database of
	   | choice installed on your machine before you begin development.
	   |
	*/
	Connection struct {
		SQLite struct {
			Driver                string `json:"driver"`
			URL                   string `json:"url" env:"DATABASE_URL"`
			Database              string `json:"database" env:"DB_DATABASE" envDefault:"database.sqlite"`
			Prefix                string `json:"prefix"`
			ForeignKeyConstraints bool   `json:"foreign_key_constraints" env:"DB_FOREIGN_KEYS" envDefault:"true"`
		} `json:"sqlite"`
		MySQL struct {
			Driver        string  `json:"driver"`
			URL           string  `json:"url" env:"DATABASE_URL"`
			Host          string  `json:"host" env:"DB_HOST" envDefault:"127.0.0.1"`
			Port          string  `json:"port" env:"DB_PORT" envDefault:"3306"`
			Database      string  `json:"database" env:"DB_DATABASE" envDefault:"forge"`
			Username      string  `json:"username" env:"DB_USERNAME" envDefault:"forge"`
			Password      string  `json:"password" env:"DB_PASSWORD" envDefault:""`
			UnixSocket    string  `json:"unix_socket" env:"DB_SOCKET" envDefault:""`
			Charset       string  `json:"charset"`
			Collation     string  `json:"collation"`
			Prefix        string  `json:"prefix"`
			PrefixIndexes bool    `json:"prefix_indexes"`
			Strict        bool    `json:"strict"`
			Engine        *string `json:"engine"`
			Options       struct {
				MYSQLAttributeSSLCA string `json:"mysql_attribute_sslca" env:"MYSQL_ATTR_SSL_CA"`
			} `json:"options"`
		} `json:"mysql"`

		PgSQL struct {
			Driver        string `json:"driver"`
			URL           string `json:"url" env:"DATABASE_URL"`
			Host          string `json:"host" env:"DB_HOST" envDefault:"localhost"`
			Port          int    `json:"port" env:"DB_PORT" envDefault:"1433"`
			Database      string `json:"database" env:"DB_DATABASE" envDefault:"forge"`
			Username      string `json:"username" env:"DB_USERNAME" envDefault:"forge"`
			Password      string `json:"password" env:"DB_PASSWORD" envDefault:""`
			Charset       string `json:"charset"`
			Prefix        string `json:"prefix"`
			PrefixIndexes bool   `json:"prefix_indexes"`
			Schema        string `json:"schema"`
			SSLMode       string `json:"sslmode"`
		} `json:"pgsql"`

		SQLServer struct {
			Driver        string `json:"driver"`
			URL           string `json:"url" env:"DATABASE_URL"`
			Host          string `json:"host" env:"DB_HOST" envDefault:"localhost"`
			Port          int    `json:"port" env:"DB_PORT" envDefault:"1433"`
			Database      string `json:"database" env:"DB_DATABASE" envDefault:"forge"`
			Username      string `json:"username" env:"DB_USERNAME" envDefault:"forge"`
			Password      string `json:"password" env:"DB_PASSWORD" envDefault:""`
			Charset       string `json:"charset"`
			Prefix        string `json:"prefix"`
			PrefixIndexes bool   `json:"prefix_indexes"`
		} `json:"sqlsrv"`
	} `json:"connection"`

	/*
	   |--------------------------------------------------------------------------
	   | Migration Repository Table
	   |--------------------------------------------------------------------------
	   |
	   | This table keeps track of all the migrations that have already run for
	   | your application. Using this information, we can determine which of
	   | the migrations on disk haven't actually been run in the database.
	   |
	*/

	Migrations string `json:"migrations"`

	/*
	   |--------------------------------------------------------------------------
	   | Redis Databases
	   |--------------------------------------------------------------------------
	   |
	   | Redis is an open source, fast, and advanced key-value store that also
	   | provides a richer body of commands than a typical key-value system
	   | such as APC or Memcached. Laravel makes it easy to dig right in.
	   |
	*/

	Redis struct {
		Client  string `json:"client" env:"REDIS_CLIENT" envDefault:"phpredis"`
		Options struct {
			Cluster string `json:"cluster" env:"REDIS_CLUSTER" envDefault:"redis"`
			Prefix  string `json:"prefix" env:"REDIS_PREFIX"`
		} `json:"options"`
		Default struct {
			URL      string `json:"url" env:"REDIS_URL"`
			Host     string `json:"host" env:"REDIS_HOST" envDefault:"127.0.0.1"`
			Password string `json:"password" env:"REDIS_PASSWORD" envDefault:""`
			Port     int    `json:"port" env:"REDIS_PORT" envDefault:"6379"`
			Database string `json:"database" env:"REDIS_DB" envDefault:"0"`
		} `json:"default"`
		Cache struct {
			URL      string `json:"url" env:"REDIS_URL"`
			Host     string `json:"host" env:"REDIS_HOST" envDefault:"127.0.0.1"`
			Password string `json:"password" env:"REDIS_PASSWORD" envDefault:""`
			Port     string `json:"port" env:"REDIS_PORT" envDefault:"6379"`
			Database string `json:"database" env:"REDIS_CACHE_DB" envDefault:"1"`
		} `json:"cache"`
	} `json:"redis"`
}
