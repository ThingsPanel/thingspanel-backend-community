package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/brokercontrol"
)

// A stable per-replica instance identifies membership; every worker startup
// mints a new boot generation before processing. This opt-in strict profile is
// unavailable to legacy workers, which require a separate controlled retirement.
func brokerRuntimeSync(db *gorm.DB, roles ...string) (func(context.Context) error, error) {
	if !viper.GetBool("mqtt.route_fencing") {
		return nil, nil
	}
	if viper.GetString("mqtt.input_profile") != "ingress_v1" || db == nil {
		return nil, errors.New("route fencing requires ingress_v1 and SQL storage")
	}
	instance, cell := viper.GetString("mqtt.runtime_instance"), viper.GetString("mqtt.broker_cell")
	if instance == "" || len(instance) > 128 || cell == "" {
		return nil, errors.New("route fencing requires a stable mqtt.runtime_instance and broker_cell")
	}
	for _, table := range []string{"broker_route_members", "broker_route_changes", "broker_route_barrier_members", "broker_route_change_events"} {
		if !db.Migrator().HasTable(table) {
			return nil, errors.New("route fencing requires SQL migration 012")
		}
	}
	store, boot := brokercontrol.NewMigrationStore(db), uuid.NewString()
	return func(parent context.Context) error {
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		return store.SyncFinalSQLRuntime(ctx, cell, instance, boot, roles)
	}, nil
}
