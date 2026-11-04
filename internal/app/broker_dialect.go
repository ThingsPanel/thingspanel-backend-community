package app

import (
	"errors"

	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
)

func requirePostgresBrokerControlPlane(db *gorm.DB) error {
	dialect, err := relational.DialectOf(db)
	if err != nil {
		return err
	}
	if dialect != relational.Postgres {
		return errors.New("broker control plane requires relational.driver=postgres")
	}
	return nil
}
