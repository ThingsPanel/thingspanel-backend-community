// metadata-upgrade-groups converts groups only in verified isolated rehearsals.
// Connection secrets come from PG* environment variables, never command flags.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"encore.app/devicemetadata/offlineupgrade"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func run() error {
	var in offlineupgrade.GroupInput
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return fmt.Errorf("invalid private conversion input")
	}
	if _, err := offlineupgrade.PlanGroups(in); err != nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	receipt, err := offlineupgrade.ApplyGroupsPG(ctx, db, in)
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
