package initialize

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	global "project/pkg/global"
)

var (
	platformRegistryMu sync.Mutex
	platformRegistry   = relational.NewRegistry()
	mysqlCasbinInit    = CasbinInit
)

// RelationalInit resolves the deployment's one relational driver and opens the
// platform resource. PostgreSQL remains the default when relational.driver is
// empty; unsupported values fail before any connection is attempted.
func RelationalInit() (*gorm.DB, error) {
	source := func(key string) string { return viper.GetString(key) }
	target, err := relational.ResolveTarget(source, "platform")
	if err != nil {
		return nil, err
	}
	var db *gorm.DB
	if target.Dialect == relational.MySQL {
		db, err = relational.Open(context.Background(), target)
	} else {
		// Preserve the platform's historical PostgreSQL reconnect window.
		for attempt := 0; attempt < 10; attempt++ {
			db, err = relational.Open(context.Background(), target)
			if err == nil {
				break
			}
			if attempt < 9 {
				logrus.Warnf("连接数据库失败 (尝试 %d/%d): %v", attempt+1, 10, err)
				logrus.Infof("将在 6s 后重试连接...")
				time.Sleep(6 * time.Second)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	configurePlatformSQLLogger(db, target.Pool)
	platformRegistryMu.Lock()
	if err := platformRegistry.Register("platform", db, true); err != nil {
		platformRegistryMu.Unlock()
		_ = relational.CloseDB(db)
		return nil, err
	}
	platformRegistryMu.Unlock()

	globalDB := db
	if err := initializePlatformSchema(context.Background(), globalDB, target.Dialect, target.Flavor); err != nil {
		_ = CloseRelationalResources()
		return nil, err
	}
	return db, nil
}

func configurePlatformSQLLogger(db *gorm.DB, pool relational.PoolOptions) {
	pool = relational.DefaultPool(pool)
	slowThreshold := pool.SlowThreshold
	if slowThreshold <= 0 {
		slowThreshold = 200 * time.Millisecond
	}
	db.Logger = logger.New(log.New(GetSQLLogWriter(), "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold:             slowThreshold,
		LogLevel:                  logger.LogLevel(pool.LogLevel),
		IgnoreRecordNotFoundError: true,
		Colorful:                  true,
	})
}

// CloseRelationalResources closes pools created by initialize. It is safe to
// call more than once and is used by the application shutdown path.
func CloseRelationalResources() error {
	platformRegistryMu.Lock()
	defer platformRegistryMu.Unlock()
	if db, ok := platformRegistry.Get("platform"); ok && global.DB == db {
		global.DB = nil
	}
	return platformRegistry.Close()
}

func initializePlatformSchema(ctx context.Context, db *gorm.DB, dialect relational.Dialect, flavor relational.Flavor) error {
	if dialect == relational.MySQL {
		if err := RequireNoTemporaryTablePrivilege(db); err != nil {
			return err
		}
		if err := ApplyMySQLFamilyPlatformSchema(ctx, db, flavor); err != nil {
			return wrapMySQLSchemaError(err)
		}
		global.DB = db
		if err := mysqlCasbinInit(); err != nil {
			return fmt.Errorf("initialize MySQL authorization adapter: %w", err)
		}
		return nil
	}
	global.DB = db
	CasbinInit()
	if err := CheckVersion(db); err != nil {
		return fmt.Errorf("检查数据库版本失败: %w", err)
	}
	for _, step := range []struct {
		name string
		run  func(*gorm.DB) error
	}{
		{"permanent device identity", ApplyDeviceIdentitySchema},
		{"service access receipt", ApplyServiceAccessSchema},
		{"telemetry history intent", ApplyHistoryIntentSchema},
		{"market installation", ApplyMarketInstallSchema},
	} {
		if err := step.run(db); err != nil {
			return fmt.Errorf("initialize %s schema: %w", step.name, err)
		}
	}
	if os.Getenv("TP_PLATFORMCONFIG_OWNER") == "encore" {
		if err := ApplyPlatformConfigSchema(db); err != nil {
			return fmt.Errorf("initialize existing platform dictionary schema: %w", err)
		}
	}
	for _, step := range []struct {
		name string
		run  func(*gorm.DB) error
	}{
		{"native binding", ApplyNativeBindingSchema},
		{"automation command guard", ApplyCommandGuardSchema},
		{"app push", ApplyAppPushSchema},
		{"auth", ApplyAuthSchema},
	} {
		if err := step.run(db); err != nil {
			return fmt.Errorf("initialize %s schema: %w", step.name, err)
		}
	}
	return nil
}

func wrapMySQLSchemaError(err error) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1419 {
		return fmt.Errorf("initialize MySQL platform schema: the device identity triggers need log_bin_trust_function_creators=1 (or the SUPER privilege for the migrating account): %w", err)
	}
	return fmt.Errorf("initialize MySQL platform schema: %w", err)
}
