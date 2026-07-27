// Copyright 2022 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sqlite // import "modernc.org/sqlite"

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"
)

var finalCalled bool

type sumFunction struct {
	sum         int64
	finalCalled *bool
}

func (f *sumFunction) Step(ctx *FunctionContext, args []driver.Value) error {
	switch resTyped := args[0].(type) {
	case int64:
		f.sum += resTyped
	default:
		return fmt.Errorf("function did not return a valid driver.Value: %T", resTyped)
	}
	return nil
}

func (f *sumFunction) WindowInverse(ctx *FunctionContext, args []driver.Value) error {
	switch resTyped := args[0].(type) {
	case int64:
		f.sum -= resTyped
	default:
		return fmt.Errorf("function did not return a valid driver.Value: %T", resTyped)
	}
	return nil
}

func (f *sumFunction) WindowValue(ctx *FunctionContext) (driver.Value, error) {
	return f.sum, nil
}

func (f *sumFunction) Final(ctx *FunctionContext) {
	*f.finalCalled = true
}

func init() {
	MustRegisterDeterministicScalarFunction(
		"test_int64",
		0,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			return int64(42), nil
		},
	)

	MustRegisterDeterministicScalarFunction(
		"test_float64",
		0,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			return float64(1e-2), nil
		},
	)

	MustRegisterDeterministicScalarFunction(
		"test_null",
		0,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			return nil, nil
		},
	)

	MustRegisterDeterministicScalarFunction(
		"test_error",
		0,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			return nil, errors.New("boom")
		},
	)

	MustRegisterDeterministicScalarFunction(
		"test_empty_byte_slice",
		0,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			return []byte{}, nil
		},
	)

	MustRegisterDeterministicScalarFunction(
		"test_nonempty_byte_slice",
		0,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			return []byte("abcdefg"), nil
		},
	)

	MustRegisterDeterministicScalarFunction(
		"test_empty_string",
		0,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			return "", nil
		},
	)

	MustRegisterDeterministicScalarFunction(
		"test_nonempty_string",
		0,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			return "abcdefg", nil
		},
	)

	MustRegisterDeterministicScalarFunction(
		"yesterday",
		1,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			var arg time.Time
			switch argTyped := args[0].(type) {
			case int64:
				arg = time.Unix(argTyped, 0)
			default:
				fmt.Println(argTyped)
				return nil, fmt.Errorf("expected argument to be int64, got: %T", argTyped)
			}
			return arg.Add(-24 * time.Hour), nil
		},
	)

	MustRegisterDeterministicScalarFunction(
		"md5",
		1,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			var arg *bytes.Buffer
			switch argTyped := args[0].(type) {
			case string:
				arg = bytes.NewBuffer([]byte(argTyped))
			case []byte:
				arg = bytes.NewBuffer(argTyped)
			default:
				return nil, fmt.Errorf("expected argument to be a string, got: %T", argTyped)
			}
			w := md5.New()
			if _, err := arg.WriteTo(w); err != nil {
				return nil, fmt.Errorf("unable to compute md5 checksum: %s", err)
			}
			return hex.EncodeToString(w.Sum(nil)), nil
		},
	)

	MustRegisterDeterministicScalarFunction(
		"regexp",
		2,
		func(ctx *FunctionContext, args []driver.Value) (driver.Value, error) {
			var s1 string
			var s2 string

			switch arg0 := args[0].(type) {
			case string:
				s1 = arg0
			default:
				return nil, errors.New("expected argv[0] to be text")
			}

			fmt.Println(args)

			switch arg1 := args[1].(type) {
			case string:
				s2 = arg1
			default:
				return nil, errors.New("expected argv[1] to be text")
			}

			matched, err := regexp.MatchString(s1, s2)
			if err != nil {
				return nil, fmt.Errorf("bad regular expression: %q", err)
			}

			return matched, nil
		},
	)

	MustRegisterFunction("test_sum", &FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		MakeAggregate: func(ctx FunctionContext) (AggregateFunction, error) {
			return &sumFunction{finalCalled: &finalCalled}, nil
		},
	})

	MustRegisterFunction("test_aggregate_error", &FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		MakeAggregate: func(ctx FunctionContext) (AggregateFunction, error) {
			return nil, errors.New("boom")
		},
	})

	MustRegisterFunction("test_aggregate_null_pointer", &FunctionImpl{
		NArgs:         1,
		Deterministic: true,
		MakeAggregate: func(ctx FunctionContext) (AggregateFunction, error) {
			return nil, nil
		},
	})
}

func TestRegisteredFunctions(t *testing.T) {
	withDB := func(test func(db *sql.DB)) {
		db, err := sql.Open("sqlite", "file::memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		finalCalled = false
		test(db)
	}

	t.Run("int64", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_int64()")

			var a int
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if g, e := a, 42; g != e {
				tt.Fatal(g, e)
			}

		})
	})

	t.Run("float64", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_float64()")

			var a float64
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if g, e := a, 1e-2; g != e {
				tt.Fatal(g, e)
			}

		})
	})

	t.Run("error", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			_, err := db.Query("select test_error()")
			if err == nil {
				tt.Fatal("expected error, got none")
			}
			if !strings.Contains(err.Error(), "boom") {
				tt.Fatal(err)
			}
		})
	})

	t.Run("empty_byte_slice", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_empty_byte_slice()")

			var a []byte
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if len(a) > 0 {
				tt.Fatal("expected empty byte slice")
			}
		})
	})

	t.Run("nonempty_byte_slice", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_nonempty_byte_slice()")

			var a []byte
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if g, e := a, []byte("abcdefg"); !bytes.Equal(g, e) {
				tt.Fatal(string(g), string(e))
			}
		})
	})

	t.Run("empty_string", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_empty_string()")

			var a string
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if len(a) > 0 {
				tt.Fatal("expected empty string")
			}
		})
	})

	t.Run("nonempty_string", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_nonempty_string()")

			var a string
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if g, e := a, "abcdefg"; g != e {
				tt.Fatal(g, e)
			}
		})
	})

	t.Run("null", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_null()")

			var a interface{}
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if a != nil {
				tt.Fatal("expected nil")
			}
		})
	})

	t.Run("dates", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select yesterday(unixepoch('2018-11-01'))")

			var a int64
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if g, e := time.Unix(a, 0), time.Date(2018, time.October, 31, 0, 0, 0, 0, time.UTC); !g.Equal(e) {
				tt.Fatal(g, e)
			}
		})
	})

	t.Run("md5", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select md5('abcdefg')")

			var a string
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if g, e := a, "7ac66c0f148de9519b8bd264312c4d64"; g != e {
				tt.Fatal(g, e)
			}
		})
	})

	t.Run("md5 with blob input", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t(b blob); insert into t values (?)", []byte("abcdefg")); err != nil {
				tt.Fatal(err)
			}
			row := db.QueryRow("select md5(b) from t")

			var a []byte
			if err := row.Scan(&a); err != nil {
				tt.Fatal(err)
			}
			if g, e := a, []byte("7ac66c0f148de9519b8bd264312c4d64"); !bytes.Equal(g, e) {
				tt.Fatal(string(g), string(e))
			}
		})
	})

	t.Run("regexp filter", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			t1 := "seafood"
			t2 := "fruit"

			if _, err := db.Exec("create table t(b text); insert into t values (?), (?)", t1, t2); err != nil {
				tt.Fatal(err)
			}
			rows, err := db.Query("select * from t where b regexp 'foo.*'")
			if err != nil {
				tt.Fatal(err)
			}

			type rec struct {
				b string
			}
			var a []rec
			for rows.Next() {
				var r rec
				if err := rows.Scan(&r.b); err != nil {
					tt.Fatal(err)
				}

				a = append(a, r)
			}
			if err := rows.Err(); err != nil {
				tt.Fatal(err)
			}

			if g, e := len(a), 1; g != e {
				tt.Fatal(g, e)
			}

			if g, e := a[0].b, t1; g != e {
				tt.Fatal(g, e)
			}
		})
	})

	t.Run("regexp matches", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select 'seafood' regexp 'foo.*'")

			var r int
			if err := row.Scan(&r); err != nil {
				tt.Fatal(err)
			}

			if g, e := r, 1; g != e {
				tt.Fatal(g, e)
			}
		})
	})

	t.Run("regexp does not match", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select 'fruit' regexp 'foo.*'")

			var r int
			if err := row.Scan(&r); err != nil {
				tt.Fatal(err)
			}

			if g, e := r, 0; g != e {
				tt.Fatal(g, e)
			}
		})
	})

	t.Run("regexp errors on bad regexp", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			_, err := db.Query("select 'seafood' regexp 'a(b'")
			if err == nil {
				tt.Fatal(errors.New("expected error, got none"))
			}
		})
	})

	t.Run("regexp errors on bad first argument", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			_, err := db.Query("SELECT 1 REGEXP 'a(b'")
			if err == nil {
				tt.Fatal(errors.New("expected error, got none"))
			}
		})
	})

	t.Run("regexp errors on bad second argument", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			_, err := db.Query("SELECT 'seafood' REGEXP 1")
			if err == nil {
				tt.Fatal(errors.New("expected error, got none"))
			}
		})
	})

	t.Run("sumFunction type error", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_sum('foo');")

			err := row.Scan()
			if err == nil {
				tt.Fatal("expected error, got none")
			}
			if !strings.Contains(err.Error(), "string") {
				tt.Fatal(err)
			}
			if !finalCalled {
				t.Error("xFinal not called")
			}
		})
	})

	t.Run("sumFunction multiple columns", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t(a int64, b int64); insert into t values (1, 5), (2, 6), (3, 7), (4, 8)"); err != nil {
				tt.Fatal(err)
			}
			row := db.QueryRow("select test_sum(a), test_sum(b) from t;")

			var a, b int64
			var e int64 = 10
			var f int64 = 26
			if err := row.Scan(&a, &b); err != nil {
				tt.Fatal(err)
			}
			if a != e {
				tt.Fatal(a, e)
			}
			if b != f {
				tt.Fatal(b, f)
			}
			if !finalCalled {
				t.Error("xFinal not called")
			}
		})
	})

	// https://www.sqlite.org/windowfunctions.html#user_defined_aggregate_window_functions
	t.Run("sumFunction as window function", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t3(x, y); insert into t3 values('a', 4), ('b', 5), ('c', 3), ('d', 8), ('e', 1);"); err != nil {
				tt.Fatal(err)
			}
			rows, err := db.Query("select x, test_sum(y) over (order by x rows between 1 preceding and 1 following) as sum_y from t3 order by x;")
			if err != nil {
				tt.Fatal(err)
			}
			defer rows.Close()

			type row struct {
				x    string
				sumY int64
			}

			got := make([]row, 0)
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.x, &r.sumY); err != nil {
					tt.Fatal(err)
				}
				got = append(got, r)
			}

			want := []row{
				{"a", 9},
				{"b", 12},
				{"c", 16},
				{"d", 12},
				{"e", 9},
			}

			if len(got) != len(want) {
				tt.Fatal(len(got), len(want))
			}

			for i, g := range got {
				if g != want[i] {
					tt.Fatal(i, g, want[i])
				}
			}
			if !finalCalled {
				t.Error("xFinal not called")
			}
		})
	})

	t.Run("aggregate function cannot be created", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_aggregate_error(1);")

			err := row.Scan()
			if err == nil {
				tt.Fatal("expected error, got none")
			}
			if !strings.Contains(err.Error(), "boom") {
				tt.Fatal(err)
			}
		})
	})

	t.Run("null aggregate function pointer", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			row := db.QueryRow("select test_aggregate_null_pointer(1);")

			err := row.Scan()
			if err == nil {
				tt.Fatal("expected error, got none")
			}
			if !strings.Contains(err.Error(), "MakeAggregate function returned nil") {
				tt.Fatal(err)
			}
		})
	})

	t.Run("serialize and deserialize", func(tt *testing.T) {
		type serializer interface {
			Serialize() ([]byte, error)
			Deserialize([]byte) error
		}

		var buf []byte

		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t(b text); insert into t values (?), (?)", "text1", "text2"); err != nil {
				tt.Fatal(err)
			}

			// Serialize the DB into buf
			func() {
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				err = conn.Raw(func(driverConn any) error {
					var err error
					buf, err = driverConn.(serializer).Serialize()
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
			}()
		})

		// Deserialize will be executed on a new in-memory DB
		withDB(func(db *sql.DB) {
			// Deserialize buf into the DB
			func() {
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				err = conn.Raw(func(driverConn any) error {
					return driverConn.(serializer).Deserialize(buf)
				})
				if err != nil {
					t.Fatal(err)
				}
			}()

			// Note sqlite3_deserialize will disconnect all connections from
			// the database, so force to recreate a connection here. It means
			// all connections attached to the db object are now invalid. Do
			// not close it because it will be closed by withDB. It's a bit
			// weird to handle.
			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			// Check the table is complete
			row := conn.QueryRowContext(context.Background(), "select count(*) from t")

			var count int
			if err := row.Scan(&count); err != nil {
				tt.Fatal(err)
			}
			if count != 2 {
				tt.Fatal(count, 2)
			}
		})
	})

	t.Run("backup and restore", func(tt *testing.T) {
		type backuper interface {
			NewBackup(string) (*Backup, error)
			NewRestore(string) (*Backup, error)
		}

		tmpDir, err := os.MkdirTemp(os.TempDir(), "storetest_")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)
		tmpFile := path.Join(tmpDir, "test.db")

		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t(b text); insert into t values (?), (?)", "text1", "text2"); err != nil {
				tt.Fatal(err)
			}

			// Backup the DB to tmpFile
			func() {
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				err = conn.Raw(func(driverConn any) error {
					bck, err := driverConn.(backuper).NewBackup(tmpFile)
					if err != nil {
						return err
					}
					for more := true; more; {
						more, err = bck.Step(-1)
						if err != nil {
							return err
						}
					}

					return bck.Finish()
				})
				if err != nil {
					t.Fatal(err)
				}
			}()
		})

		// Restore will be executed on a new in-memory DB
		withDB(func(db *sql.DB) {
			func() {
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				err = conn.Raw(func(driverConn any) error {
					bck, err := driverConn.(backuper).NewRestore(tmpFile)
					if err != nil {
						return err
					}
					for more := true; more; {
						more, err = bck.Step(-1)
						if err != nil {
							return err
						}
					}

					return bck.Finish()
				})
				if err != nil {
					t.Fatal(err)
				}
			}()

			// Check the table is complete
			var count int
			row := db.QueryRow("select count(*) from t")
			if err := row.Scan(&count); err != nil {
				tt.Fatal(err)
			} else if count != 2 {
				tt.Fatal(count, 2)
			}
		})
	})

	t.Run("backup, commit and close", func(tt *testing.T) {
		type backuper interface {
			NewBackup(string) (*Backup, error)
			NewRestore(string) (*Backup, error)
		}

		tmpDir, err := os.MkdirTemp(os.TempDir(), "storetest_")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		var inMemDB driver.Conn

		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t(b text); insert into t values (?), (?)", "text1", "text2"); err != nil {
				tt.Fatal(err)
			}

			// Backup the DB to an in-memory instance
			func() {
				conn, err := db.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				err = conn.Raw(func(driverConn any) error {
					bck, err := driverConn.(backuper).NewBackup(":memory:")
					if err != nil {
						return err
					}
					for more := true; more; {
						more, err = bck.Step(-1)
						if err != nil {
							return err
						}
					}

					inMemDB, err = bck.Commit()

					return err
				})
				if err != nil {
					t.Fatal(err)
				}
			}()
		})

		if inMemDB == nil {
			tt.Fatal("in-memory database must not be nil")
		}

		querier, ok := (inMemDB).(interface {
			Query(query string, args []driver.Value) (dr driver.Rows, err error)
		})

		if !ok {
			tt.Fatal(fmt.Errorf("in-memory DB does not implement a Query method: %T", inMemDB))
		}

		// Check the table is complete
		rows, err := querier.Query("select count(*) from t", nil)
		if err != nil {
			tt.Fatal(err)
		}

		var count int
		var values = []driver.Value{&count}

		err = rows.Next(values)
		if err != nil {
			tt.Fatal(err)
		}

		if (values[0]).(int64) != 2 {
			tt.Fatal(count, 2)
		}

		err = rows.Next(values)
		if !errors.Is(err, io.EOF) {
			tt.Fatal(err)
		}

		if err = rows.Close(); err != nil {
			tt.Fatal(err)
		}

		if err = inMemDB.Close(); err != nil {
			tt.Fatal(err)
		}
	})

	t.Run("QueryContext with context expired", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t(b text); insert into t values (?), (?)", "text1", "text2"); err != nil {
				tt.Fatal(err)
			}

			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = conn.QueryContext(ctx, "select count(*) from t")
			if err == nil {
				tt.Fatalf("unexpected success while context is canceled")
			}
		})
	})

	t.Run("QueryContext with context expiring", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t(b text); insert into t values (?), (?)", "text1", "text2"); err != nil {
				tt.Fatal(err)
			}

			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			for try := 0; try < 1000; try++ {
				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					time.Sleep(100 * time.Millisecond)
					cancel()
				}()

				for start := time.Now(); time.Since(start) < 200*time.Millisecond; {
					func() {
						rows, err := conn.QueryContext(ctx, "select count(*) from t")
						if rows != nil {
							defer rows.Close()
						}
						if err != nil && !(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "interrupted (9)")) {
							tt.Fatalf("unexpected error, was expected context or interrupted error: err=%v", err)
						} else if err == nil && !rows.Next() {
							if rowsErr := rows.Err(); rowsErr != nil && !(errors.Is(rowsErr, context.Canceled) || errors.Is(rowsErr, context.DeadlineExceeded) || strings.Contains(rowsErr.Error(), "interrupted (9)")) {
								tt.Fatalf("unexpected error, was expected context or interrupted error: err=%v", err)
							} else if rowsErr == nil {
								tt.Fatalf("success with no data (try=%d)", try)
							}
						}
					}()
				}
			}
		})
	})

	t.Run("ExecContext with context expired", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t(b text); insert into t values (?), (?)", "text1", "text2"); err != nil {
				tt.Fatal(err)
			}

			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = conn.ExecContext(ctx, "insert into t values (?)", "text3")
			if err == nil {
				tt.Fatalf("unexpected success while context is canceled")
			} else if !(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "interrupted (9)")) {
				tt.Fatalf("unexpected error while context is canceled: expected context or interrupted, got %v", err)
			}
		})
	})

	t.Run("ExecContext with context expiring", func(tt *testing.T) {
		withDB(func(db *sql.DB) {
			if _, err := db.Exec("create table t(b text); insert into t values (?), (?)", "text1", "text2"); err != nil {
				tt.Fatal(err)
			}

			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			for try := 0; try < 1000; try++ {
				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					time.Sleep(100 * time.Millisecond)
					cancel()
				}()

				for start := time.Now(); time.Since(start) < 200*time.Millisecond; {
					res, err := conn.ExecContext(ctx, "insert into t values (?)", "text3")
					if err != nil && !(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "interrupted (9)")) {
						tt.Fatalf("unexpected error, was expected context or interrupted error: err=%v", err)
					} else if err == nil {
						if rowsAffected, err := res.RowsAffected(); err != nil {
							tt.Fatal(err)
						} else if rowsAffected != 1 {
							tt.Fatalf("success with no inserted data (try=%d)", try)
						}
					}
				}
			}
		})
	})
}
