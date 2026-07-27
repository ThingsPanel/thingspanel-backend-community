// Copyright 2024 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "modernc.org/sqlite"

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFcntlPersistWAL(t *testing.T) {
	t.Run("WAL is cleaned up without persist WAL", func(t *testing.T) {
		name := filepath.Join(t.TempDir(), "tmp.db")
		walName := name + "-wal"
		db, err := sql.Open(driverName, fmt.Sprintf("file:%s", name))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		// enable WAL journal
		if _, err := db.Exec("pragma journal_mode = WAL"); err != nil {
			t.Fatal(err)
		}

		if _, err := db.Exec("create table t(b int)"); err != nil {
			t.Fatal(err)
		}

		// database file must exist after creating a table
		if _, err := os.Stat(name); err != nil {
			t.Fatal(err)
		}

		// wal file must exist after creating a table
		if _, err := os.Stat(walName); err != nil {
			t.Fatal(err)
		}

		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		// database file must exist after closing it
		if _, err := os.Stat(name); err != nil {
			t.Fatal(err)
		}

		// wal file must NOT exist after closing the db
		if _, err := os.Stat(walName); err == nil {
			t.Errorf("expected WAL file %s to not exist after closing db", walName)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	})

	t.Run("WAL is not cleaned up with persist WAL", func(t *testing.T) {
		name := filepath.Join(t.TempDir(), "tmp.db")
		walName := name + "-wal"
		db, err := sql.Open(driverName, fmt.Sprintf("file:%s", name))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		conn, err := db.Conn(context.TODO())
		if err != nil {
			t.Fatal(err)
		}

		// enable persist WAL for a connection, normally this is done with a hook
		err = conn.Raw(func(driverConn any) error {
			fc, ok := driverConn.(FileControl)
			if !ok {
				return fmt.Errorf("driver connection didn't implement FileControl")
			}

			// query
			mode, err := fc.FileControlPersistWAL("main", -1)
			if err != nil {
				return fmt.Errorf("file control call failed: %w", err)
			} else if mode != 0 {
				return fmt.Errorf("file control call returned unexpected mode: %d", mode)
			}

			// enable
			mode, err = fc.FileControlPersistWAL("main", 1)
			if err != nil {
				return fmt.Errorf("file control call failed: %w", err)
			} else if mode != 1 {
				return fmt.Errorf("file control call returned unexpected mode: %d", mode)
			}

			// verify
			mode, err = fc.FileControlPersistWAL("main", -1)
			if err != nil {
				return fmt.Errorf("file control call failed: %w", err)
			} else if mode != 1 {
				return fmt.Errorf("file control call returned unexpected mode: %d", mode)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := conn.ExecContext(context.TODO(), "pragma journal_mode = WAL"); err != nil {
			t.Fatal(err)
		}

		if _, err := conn.ExecContext(context.TODO(), "create table t(b int)"); err != nil {
			t.Fatal(err)
		}

		// database file must exist after creating a table
		if _, err := os.Stat(name); err != nil {
			t.Fatal(err)
		}

		// wal file must exist after creating a table
		if _, err := os.Stat(walName); err != nil {
			t.Fatal(err)
		}

		// close connection, should persist WAL
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}

		// close database, should persist WAL
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		// database file must exist after closing it
		if _, err := os.Stat(name); err != nil {
			t.Fatal(err)
		}

		// wal file must exist after closing the db
		if _, err := os.Stat(walName); err != nil {
			t.Errorf("expected WAL file %s to exist after closing db: %s", walName, err.Error())
		}
	})
}
