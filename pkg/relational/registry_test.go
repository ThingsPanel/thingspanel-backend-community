package relational

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync/atomic"
	"testing"
)

// fakeDriver hands out connections that count Close, so the tests can see which pools the registry closed.
type fakeDriver struct{ closed *atomic.Int32 }

func (d fakeDriver) Open(string) (driver.Conn, error) { return fakeConn{d.closed}, nil }

type fakeConn struct{ closed *atomic.Int32 }

func (c fakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c fakeConn) Close() error                        { c.closed.Add(1); return nil }
func (c fakeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

var fakeSeq atomic.Int32

func fakePool(t *testing.T) (*sql.DB, *atomic.Int32) {
	t.Helper()
	closed := new(atomic.Int32)
	name := "relational-fake-" + string(rune('a'+fakeSeq.Add(1)))
	sql.Register(name, fakeDriver{closed})
	pool, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.PingContext(context.Background()); err != nil { // makes one idle connection
		t.Fatal(err)
	}
	return pool, closed
}

func TestAdoptSetsDialectAndPool(t *testing.T) {
	for _, d := range []Dialect{Postgres, MySQL} {
		pool, _ := fakePool(t)
		db, err := Adopt(d, pool, PoolOptions{MaxOpenConns: 4, MaxIdleConns: 2})
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		if got, err := DialectOf(db); err != nil || got != d {
			t.Errorf("DialectOf = %q, %v; want %q", got, err, d)
		}
		if st := pool.Stats(); st.MaxOpenConnections != 4 {
			t.Errorf("%s: max open = %d", d, st.MaxOpenConnections)
		}
		_ = pool.Close()
	}
	if _, err := Adopt(Postgres, nil, PoolOptions{}); err == nil {
		t.Error("nil pool accepted")
	}
	if _, err := Adopt("oracle", &sql.DB{}, PoolOptions{}); err == nil {
		t.Error("bad dialect accepted")
	}
}

func TestRegistryOnePoolPerResourceAndOnlyOwnedAreClosed(t *testing.T) {
	ownedPool, ownedClosed := fakePool(t)
	adoptedPool, adoptedClosed := fakePool(t)
	owned, err := Adopt(Postgres, ownedPool, PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := Adopt(Postgres, adoptedPool, PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	if err := r.Register("platform", owned, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("device_metadata", adopted, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("platform", owned, true); err == nil {
		t.Fatal("a second pool for the same resource was accepted")
	}
	if got, ok := r.Get("platform"); !ok || got != owned {
		t.Fatal("Get")
	}
	if names := r.Names(); len(names) != 2 || names[0] != "device_metadata" || names[1] != "platform" {
		t.Fatalf("%v", names)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if ownedClosed.Load() == 0 {
		t.Error("the owned pool was not closed")
	}
	if adoptedClosed.Load() != 0 {
		t.Error("an adopted pool was closed by the registry")
	}
	if _, ok := r.Get("platform"); ok {
		t.Error("registry kept an entry after Close")
	}
	_ = adoptedPool.Close()
}

func TestDialectOfRejectsNil(t *testing.T) {
	if _, err := DialectOf(nil); err == nil {
		t.Fatal("nil accepted")
	}
}
