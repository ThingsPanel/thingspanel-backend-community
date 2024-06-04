package dal

import (
	"project/global"
	"project/query"
)

func StartTransaction() (*query.QueryTx, error) {
	tx := query.Use(global.DB).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	return tx, nil
}

func Rollback(tx *query.QueryTx) error {
	if err := tx.Rollback(); err != nil {
		return err
	}
	return nil
}

func Commit(tx *query.QueryTx) error {
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
