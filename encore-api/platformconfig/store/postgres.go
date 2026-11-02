// Package store implements the single PostgreSQL dictionary repository. The
// database pool is injected by the host; no legacy global connection is used.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"encore.app/platformconfig/app"
)

type transaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	Commit() error
	Rollback() error
}

type Store struct {
	db    *sql.DB
	begin func(context.Context, *sql.TxOptions) (transaction, error)
}

func New(db *sql.DB) *Store {
	store := &Store{db: db}
	if db != nil {
		store.begin = func(ctx context.Context, opts *sql.TxOptions) (transaction, error) { return db.BeginTx(ctx, opts) }
	}
	return store
}

func (s *Store) available(ctx context.Context) error {
	if ctx == nil || s == nil || s.db == nil || s.begin == nil {
		return app.ErrUnavailable
	}
	return ctx.Err()
}

func persistenceError(err error) error {
	if err == nil {
		return nil
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) && state.SQLState() == "23505" {
		return fmt.Errorf("%w: %w", app.ErrConflict, err)
	}
	return err
}

// Every outcome is settled explicitly. A failed Commit acknowledgement never
// becomes success or an assumed rollback. A failed rollback also remains
// unknown. Creates keep their legacy no-key input and are not safe auto-retries.
func (s *Store) transact(ctx context.Context, opts *sql.TxOptions, apply func(transaction) error) (result error) {
	if err := s.available(ctx); err != nil {
		return err
	}
	tx, err := s.begin(ctx, opts)
	if err != nil {
		return persistenceError(err)
	}
	settled := false
	defer func() {
		if !settled {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				result = errors.Join(result, fmt.Errorf("%w: rollback acknowledgement failed: %w", app.ErrOutcomeUnknown, rollbackErr))
			}
		}
	}()
	if err = apply(tx); err != nil {
		return persistenceError(err)
	}
	err = tx.Commit()
	settled = true
	if err != nil {
		return fmt.Errorf("%w: commit acknowledgement failed: %w", app.ErrOutcomeUnknown, err)
	}
	return nil
}

func exactlyOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return app.ErrWriteNotApplied
	}
	return nil
}

func (s *Store) CreateDictionary(ctx context.Context, row app.Dictionary) error {
	return s.transact(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx transaction) error {
		if err := exactlyOne(tx.ExecContext(ctx, `INSERT INTO sys_dict (id,dict_code,dict_value,created_at,remark) VALUES ($1,$2,$3,$4,$5)`, row.ID, row.DictCode, row.DictValue, row.CreatedAt, row.Remark)); err != nil {
			return err
		}
		var saved app.Dictionary
		if err := tx.QueryRowContext(ctx, `SELECT id,dict_code,dict_value,created_at,remark FROM sys_dict WHERE id=$1 FOR UPDATE`, row.ID).Scan(&saved.ID, &saved.DictCode, &saved.DictValue, &saved.CreatedAt, &saved.Remark); err != nil {
			return err
		}
		if saved.ID != row.ID || saved.DictCode != row.DictCode || saved.DictValue != row.DictValue || !saved.CreatedAt.Equal(row.CreatedAt) || !sameString(saved.Remark, row.Remark) {
			return app.ErrWriteNotApplied
		}
		return nil
	})
}

func sameString(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func (s *Store) CreateLanguage(ctx context.Context, row app.Language) error {
	return s.transact(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx transaction) error {
		// Parent first is shared with both deletion operations. A concurrent
		// parent DELETE either wins first or waits for this entire transaction.
		var parent string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM sys_dict WHERE id=$1 FOR UPDATE`, row.DictID).Scan(&parent); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return app.ErrNotFound
			}
			return err
		}
		if err := exactlyOne(tx.ExecContext(ctx, `INSERT INTO sys_dict_language (id,dict_id,language_code,translation) VALUES ($1,$2,$3,$4)`, row.ID, row.DictID, row.LanguageCode, row.Translation)); err != nil {
			return err
		}
		var saved app.Language
		if err := tx.QueryRowContext(ctx, `SELECT id,dict_id,language_code,translation FROM sys_dict_language WHERE id=$1 FOR UPDATE`, row.ID).Scan(&saved.ID, &saved.DictID, &saved.LanguageCode, &saved.Translation); err != nil {
			return err
		}
		if saved != row {
			return app.ErrWriteNotApplied
		}
		return nil
	})
}

func (s *Store) DeleteDictionary(ctx context.Context, id string) error {
	return s.transact(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx transaction) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM sys_dict WHERE id=$1 FOR UPDATE`, id).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		} // deletion replay, no new side effect
		if err != nil {
			return err
		}
		if err := exactlyOne(tx.ExecContext(ctx, `DELETE FROM sys_dict WHERE id=$1`, id)); err != nil {
			return err
		}
		var remains bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sys_dict WHERE id=$1) OR EXISTS(SELECT 1 FROM sys_dict_language WHERE dict_id=$1)`, id).Scan(&remains); err != nil {
			return err
		}
		if remains {
			return app.ErrWriteNotApplied
		}
		return nil
	})
}

func (s *Store) DeleteLanguage(ctx context.Context, id string) error {
	return s.transact(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(tx transaction) error {
		// Read only a hint before taking any row lock, preserving parent->child
		// order even when a cascading parent deletion races this request.
		var parent string
		err := tx.QueryRowContext(ctx, `SELECT dict_id FROM sys_dict_language WHERE id=$1`, id).Scan(&parent)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var lockedParent string
		err = tx.QueryRowContext(ctx, `SELECT id FROM sys_dict WHERE id=$1 FOR UPDATE`, parent).Scan(&lockedParent)
		if errors.Is(err, sql.ErrNoRows) {
			var childRemains bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sys_dict_language WHERE id=$1)`, id).Scan(&childRemains); err != nil {
				return err
			}
			if childRemains {
				return app.ErrWriteNotApplied
			}
			return nil
		}
		if err != nil {
			return err
		}
		var currentParent string
		err = tx.QueryRowContext(ctx, `SELECT dict_id FROM sys_dict_language WHERE id=$1 FOR UPDATE`, id).Scan(&currentParent)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if currentParent != parent {
			return app.ErrConflict
		}
		if err := exactlyOne(tx.ExecContext(ctx, `DELETE FROM sys_dict_language WHERE id=$1 AND dict_id=$2`, id, parent)); err != nil {
			return err
		}
		var remains bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sys_dict_language WHERE id=$1)`, id).Scan(&remains); err != nil {
			return err
		}
		if remains {
			return app.ErrWriteNotApplied
		}
		return nil
	})
}

func (s *Store) Enum(ctx context.Context, code, language string) ([]app.EnumValue, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.dict_value,COALESCE(l.translation,'') FROM sys_dict d LEFT JOIN sys_dict_language l ON l.dict_id=d.id AND l.language_code=$2 WHERE d.dict_code=$1 ORDER BY d.created_at,d.id`, code, language)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer rows.Close()
	var values []app.EnumValue // legacy empty enumeration serializes as null
	for rows.Next() {
		var value app.EnumValue
		if err := rows.Scan(&value.DictValue, &value.Translation); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func (s *Store) Page(ctx context.Context, input app.PageInput) (app.Page, error) {
	result := app.Page{List: []app.Dictionary{}}
	var code any
	if input.DictCode != nil {
		code = *input.DictCode
	}
	err := s.transact(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}, func(tx transaction) error {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_dict WHERE ($1::text IS NULL OR dict_code=$1)`, code).Scan(&result.Total); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,dict_code,dict_value,created_at,remark FROM sys_dict WHERE ($1::text IS NULL OR dict_code=$1) ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, code, input.PageSize, (input.Page-1)*input.PageSize)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row app.Dictionary
			if err := rows.Scan(&row.ID, &row.DictCode, &row.DictValue, &row.CreatedAt, &row.Remark); err != nil {
				return err
			}
			result.List = append(result.List, row)
		}
		return rows.Err()
	})
	if err != nil {
		return app.Page{}, err
	}
	return result, nil
}

func (s *Store) Languages(ctx context.Context, id string) ([]app.Language, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,dict_id,language_code,translation FROM sys_dict_language WHERE dict_id=$1 ORDER BY language_code,id`, id)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer rows.Close()
	values := []app.Language{}
	for rows.Next() {
		var row app.Language
		if err := rows.Scan(&row.ID, &row.DictID, &row.LanguageCode, &row.Translation); err != nil {
			return nil, err
		}
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func (s *Store) Protocols(ctx context.Context, language string) ([]app.ProtocolRow, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	// The legacy LEFT JOIN plus WHERE on language excluded untranslated rows.
	rows, err := s.db.QueryContext(ctx, `SELECT d.dict_code,d.dict_value,l.translation FROM sys_dict d JOIN sys_dict_language l ON l.dict_id=d.id WHERE d.dict_code IN ('DRIECT_ATTACHED_PROTOCOL','GATEWAY_PROTOCOL') AND l.language_code=$1 ORDER BY d.created_at,d.id`, language)
	if err != nil {
		return nil, persistenceError(err)
	}
	defer rows.Close()
	var values []app.ProtocolRow
	for rows.Next() {
		var row app.ProtocolRow
		if err := rows.Scan(&row.Code, &row.Value.DictValue, &row.Value.Translation); err != nil {
			return nil, err
		}
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}
