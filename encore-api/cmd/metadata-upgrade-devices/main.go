// metadata-upgrade-devices stages device identities in isolated rehearsals.
// It does not publish access configuration or activate credentials/runtimes.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"encore.app/devicemetadata/offlineupgrade"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func run() error {
	var in offlineupgrade.DeviceIdentityInput
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil {
		return fmt.Errorf("invalid private conversion input")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("unexpected trailing conversion input")
	}
	if _, err := offlineupgrade.PlanDeviceIdentities(in); err != nil {
		return err
	}
	if os.Getenv("PGDATABASE") != in.TargetDatabase {
		return fmt.Errorf("PGDATABASE must match isolated target")
	}
	db, err := gorm.Open(postgres.Open(""), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("cannot connect to isolated target")
	}
	connection, err := db.DB()
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	receipt, err := offlineupgrade.ApplyDeviceIdentitiesPG(ctx, db, in)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(receipt)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
