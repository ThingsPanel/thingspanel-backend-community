package relational

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// Configuration keys. A process reads them through a Source, so the platform (viper) and the Encore app (environment
// variables) share one definition of what the keys mean.
//
//	relational.driver                     postgres (default) | mysql          one value for the whole deployment
//	relational.<engine>.<resource>.dsn    a complete DSN for one logical resource
//	relational.<engine>.<field>           structured settings shared by every resource of that engine:
//	                                      host, port, username, password, time_zone, open_conns, idle_conns,
//	                                      conn_max_lifetime, log_level, slow_threshold_ms
//	relational.<engine>.<resource>.<field> the same fields for one resource (database is normally set here)
//
// <engine> is "postgres" or "mysql"; <resource> is the logical resource name ("platform", "device_metadata", ...).
const KeyDriver = "relational.driver"

// Source returns the raw value of a configuration key, or "" when it is not set.
type Source func(key string) string

// EnvSource reads keys from environment variables: "relational.mysql.platform.dsn" is TP_RELATIONAL_MYSQL_PLATFORM_DSN.
func EnvSource() Source {
	return func(key string) string { return os.Getenv(EnvName(key)) }
}

// EnvName maps a configuration key to its environment variable.
func EnvName(key string) string {
	return "TP_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(key))
}

// MapSource reads keys from a map (tests, and callers that already hold the values).
func MapSource(m map[string]string) Source { return func(key string) string { return m[key] } }

// ResolveDialect reads relational.driver. It is the only place a driver name is turned into a Dialect; every process
// calls it once at start-up (through ResolveTarget or directly) and passes the result down.
func ResolveDialect(src Source) (Dialect, error) { return ParseDialect(src(KeyDriver)) }

// PoolOptions are the pool settings of one logical resource. Zero fields take the defaults of DefaultPool.
type PoolOptions struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	// LogLevel is GORM's logger level: 1 silent, 2 error, 3 warn, 4 info. 0 means error.
	LogLevel int
	// SlowThreshold logs statements slower than this at warn level. 0 disables.
	SlowThreshold time.Duration
}

// DefaultPool returns o with the defaults filled in.
func DefaultPool(o PoolOptions) PoolOptions {
	if o.MaxOpenConns <= 0 {
		o.MaxOpenConns = 50
	}
	if o.MaxIdleConns <= 0 {
		o.MaxIdleConns = 10
	}
	if o.MaxIdleConns > o.MaxOpenConns {
		o.MaxIdleConns = o.MaxOpenConns
	}
	if o.ConnMaxLifetime <= 0 {
		o.ConnMaxLifetime = time.Hour
	}
	if o.LogLevel == 0 {
		o.LogLevel = 2
	}
	return o
}

// Target is everything needed to open one logical resource (one connection pool).
type Target struct {
	// Name is the logical resource identity: "platform", "device_metadata", "automation", ... It keys the Registry,
	// the configuration keys and the migration history; it is never derived from the database name.
	Name    string
	Dialect Dialect

	// DSN, when set, is used as is (pgx URL or key/value string for Postgres; go-sql-driver DSN for MySQL). The
	// structured fields below are ignored.
	DSN string

	Host     string
	Port     int
	Database string
	Username string
	Password string
	// TimeZone is the session time zone ("Asia/Shanghai"). MySQL also uses it to read DATETIME values.
	TimeZone string

	Pool PoolOptions
}

// Validate checks the target without connecting.
func (t Target) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("relational: target has no name")
	}
	if !t.Dialect.Valid() {
		return fmt.Errorf("relational: target %q has no valid dialect", t.Name)
	}
	if t.DSN == "" && (t.Database == "" || t.Username == "") {
		return fmt.Errorf("relational: target %q needs a DSN or both database and username", t.Name)
	}
	if t.DSN == "" && t.Port < 0 {
		return fmt.Errorf("relational: target %q has a negative port", t.Name)
	}
	return nil
}

// ResolveTarget builds the Target of one resource from src. The dialect comes from relational.driver; the DSN or the
// structured fields come from the keys of that dialect, the resource-specific key winning over the shared one.
func ResolveTarget(src Source, resource string) (Target, error) {
	d, err := ResolveDialect(src)
	if err != nil {
		return Target{}, err
	}
	return resolveTarget(src, resource, d)
}

func resolveTarget(src Source, resource string, d Dialect) (Target, error) {
	get := func(field string) string {
		if v := strings.TrimSpace(src("relational." + string(d) + "." + resource + "." + field)); v != "" {
			return v
		}
		return strings.TrimSpace(src("relational." + string(d) + "." + field))
	}
	atoi := func(field string) (int, error) {
		v := get(field)
		if v == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("relational.%s.%s: %q is not a number", d, field, v)
		}
		return n, nil
	}
	var err error
	t := Target{Name: resource, Dialect: d, DSN: strings.TrimSpace(src("relational." + string(d) + "." + resource + ".dsn"))}
	t.Host, t.Database, t.Username, t.Password, t.TimeZone = get("host"), get("database"), get("username"), get("password"), get("time_zone")
	for field, dst := range map[string]*int{
		"port": &t.Port, "open_conns": &t.Pool.MaxOpenConns, "idle_conns": &t.Pool.MaxIdleConns, "log_level": &t.Pool.LogLevel,
	} {
		if *dst, err = atoi(field); err != nil {
			return Target{}, err
		}
	}
	if ms, err := atoi("slow_threshold_ms"); err != nil {
		return Target{}, err
	} else {
		t.Pool.SlowThreshold = time.Duration(ms) * time.Millisecond
	}
	if v := get("conn_max_lifetime"); v != "" {
		if t.Pool.ConnMaxLifetime, err = time.ParseDuration(v); err != nil {
			return Target{}, fmt.Errorf("relational.%s.conn_max_lifetime: %w", d, err)
		}
	}
	if t.DSN == "" {
		if t.Host == "" {
			t.Host = "localhost"
		}
		if t.Port == 0 {
			t.Port = map[Dialect]int{Postgres: 5432, MySQL: 3306}[d]
		}
		if t.TimeZone == "" {
			t.TimeZone = "Asia/Shanghai"
		}
	}
	if err := t.Validate(); err != nil {
		return Target{}, err
	}
	return t, nil
}

// dsn returns the connection string for the target. A structured MySQL target always gets utf8mb4, parseTime and
// clientFoundRows (so RowsAffected counts matched rows, as PostgreSQL does). The password is never logged.
func (t Target) dsn() (string, error) {
	if t.DSN != "" {
		if t.Dialect == MySQL {
			cfg, err := mysqldriver.ParseDSN(t.DSN)
			if err != nil {
				return "", fmt.Errorf("relational: target %q: invalid MySQL DSN", t.Name)
			}
			cfg.ParseTime, cfg.ClientFoundRows = true, true
			return cfg.FormatDSN(), nil
		}
		return t.DSN, nil
	}
	hostPort := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	switch t.Dialect {
	case MySQL:
		loc, err := timeLocation(t.TimeZone)
		if err != nil {
			return "", fmt.Errorf("relational: target %q: %w", t.Name, err)
		}
		cfg := mysqldriver.NewConfig()
		cfg.User, cfg.Passwd, cfg.Net, cfg.Addr, cfg.DBName = t.Username, t.Password, "tcp", hostPort, t.Database
		cfg.ParseTime, cfg.ClientFoundRows, cfg.Loc = true, true, loc
		cfg.Params = map[string]string{"charset": "utf8mb4"}
		cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 10*time.Second, 30*time.Second, 30*time.Second
		return cfg.FormatDSN(), nil
	case Postgres:
		u := url.URL{Scheme: "postgres", User: url.UserPassword(t.Username, t.Password), Host: hostPort, Path: "/" + t.Database}
		q := url.Values{}
		q.Set("sslmode", "disable")
		if t.TimeZone != "" {
			q.Set("TimeZone", t.TimeZone)
		}
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return "", fmt.Errorf("relational: target %q has no valid dialect", t.Name)
}

func timeLocation(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("invalid time zone %q", name)
	}
	return loc, nil
}
