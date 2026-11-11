// metadata-upgrade-credentials preserves existing credentials only in an isolated
// rehearsal. Secrets enter through stdin/environment and never appear in receipts.
package main

import (
	"context"
	"encoding/json"
	"encore.app/devicemetadata/credentialsecret"
	"encore.app/devicemetadata/offlineupgrade"
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io"
	"os"
	"time"
)

func run() error {
	var input offlineupgrade.CredentialInput
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("invalid private credential conversion input")
	}
	if os.Getenv("PGDATABASE") != input.TargetDatabase {
		return fmt.Errorf("PGDATABASE must match isolated target")
	}
	protector, err := credentialsecret.NewCredentialSecretProtectorFromEnv()
	if err != nil {
		return err
	}
	db, err := gorm.Open(postgres.Open(""), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("cannot connect to isolated target")
	}
	connection, err := db.DB()
	if err != nil {
		return fmt.Errorf("cannot initialize isolated connection")
	}
	defer connection.Close()
	connection.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := offlineupgrade.ApplyCredentialsPG(ctx, db, input, protector)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
