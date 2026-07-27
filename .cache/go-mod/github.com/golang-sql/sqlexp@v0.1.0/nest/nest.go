// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// package nest supports nested transactions allowing a common querier
// to be defined.
package nest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	"github.com/golang-sql/sqlexp"
)

// Wrap a sql.DB with a nestable DB.
func Wrap(db *sql.DB) *DB {
	return &DB{db: db}
}

type TxOptions = sql.TxOptions
type Result = sql.Result
type Rows = sql.Rows
type Row = sql.Row
type Stmt = sql.Stmt

// Querier is the common interface to execute queries on a DB, Tx, or Conn.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	PingContext(ctx context.Context) error
	PrepareContext(ctx context.Context, query string) (*Stmt, error)
	Commit() error
	Rollback() error
}

var (
	_ Querier = &DB{}
	_ Querier = &Tx{}
	_ Querier = &Conn{}
)

type DB struct {
	db *sql.DB
}

func (db *DB) BeginTx(ctx context.Context, opts *TxOptions) (*Tx, error) {
	tx, err := db.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{ctx: ctx, tx: tx, db: db, savepointer: savepointFromDriver(db.db.Driver())}, nil
}

func (db *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (Result, error) {
	return db.db.ExecContext(ctx, query, args...)
}

func (db *DB) PingContext(ctx context.Context) error {
	return db.db.PingContext(ctx)
}

func (db *DB) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	return db.db.PrepareContext(ctx, query)
}

func (db *DB) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	return db.db.QueryContext(ctx, query, args...)
}

func (db *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	return db.db.QueryRowContext(ctx, query, args...)
}

var errNoTx = errors.New("sqlexp/nest: not in a transaction")
var errNoNested = errors.New("sqlexp/nest: nested transactions not supported")

func (db *DB) Commit() error {
	return errNoTx
}

func (db *DB) Rollback() error {
	return errNoTx
}

func (db *DB) DB() *sql.DB {
	return db.db
}

type Tx struct {
	ctx   context.Context
	db    *DB
	tx    *sql.Tx
	index int

	savepointer sqlexp.Savepointer
	savepoint   string // Empty if not in a snapshot, otherwise the name of the snapshot.
}

func savepointFromDriver(d driver.Driver) sqlexp.Savepointer {
	sp, _ := sqlexp.SavepointFromDriver(d)
	return sp
}

func (tx *Tx) BeginTx(ctx context.Context, opts *TxOptions) (*Tx, error) {
	if tx.savepointer == nil {
		return nil, errNoNested
	}
	index := tx.index + 1
	savepoint := fmt.Sprintf("savept%dx", index)

	_, err := tx.tx.ExecContext(tx.ctx, tx.savepointer.Create(savepoint))
	if err != nil {
		return nil, err
	}
	return &Tx{ctx: ctx, tx: tx.tx, savepoint: savepoint, index: index}, nil
}

func (tx *Tx) ExecContext(ctx context.Context, query string, args ...interface{}) (Result, error) {
	return tx.tx.ExecContext(ctx, query, args...)
}

func (tx *Tx) PingContext(ctx context.Context) error {
	return tx.db.PingContext(ctx)
}

func (tx *Tx) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	return tx.tx.PrepareContext(ctx, query)
}
func (tx *Tx) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	return tx.tx.QueryContext(ctx, query, args...)
}
func (tx *Tx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	return tx.tx.QueryRowContext(ctx, query, args)
}

func (tx *Tx) Commit() error {
	if len(tx.savepoint) == 0 {
		return tx.tx.Commit()
	}
	// Not all databases support savepoint release.
	q := tx.savepointer.Release(tx.savepoint)
	if q == "" {
		return nil
	}
	_, err := tx.tx.ExecContext(tx.ctx, q)
	return err
}

func (tx *Tx) Rollback() error {
	if len(tx.savepoint) == 0 {
		return tx.tx.Rollback()
	}
	_, err := tx.tx.ExecContext(tx.ctx, tx.savepointer.Rollback(tx.savepoint))
	return err
}

func (tx *Tx) Tx() *sql.Tx {
	return tx.tx
}

type Conn struct {
	db   *DB
	conn *sql.Conn
}

func (c *Conn) BeginTx(ctx context.Context, opts *TxOptions) (*Tx, error) {
	tx, err := c.conn.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{ctx: ctx, tx: tx, db: c.db, savepointer: savepointFromDriver(c.db.db.Driver())}, nil
}

func (c *Conn) ExecContext(ctx context.Context, query string, args ...interface{}) (Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func (c *Conn) PingContext(ctx context.Context) error {
	return c.conn.PingContext(ctx)
}

func (c *Conn) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	return c.conn.PrepareContext(ctx, query)
}

func (c *Conn) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c *Conn) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	return c.conn.QueryRowContext(ctx, query, args...)
}

func (c *Conn) Commit() error {
	return errNoTx
}

func (c *Conn) Rollback() error {
	return errNoTx
}

func (c *Conn) Conn() *sql.Conn {
	return c.conn
}
