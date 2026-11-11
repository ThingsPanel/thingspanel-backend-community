package store_test

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/marketinstall/app"
	"encore.app/marketinstall/store"
	"encore.app/pkg/ownermigration"
	"github.com/go-sql-driver/mysql"
)

// This fixture forwards the actual MySQL protocol to the isolated server. It
// drops only the server's OK after an armed COMMIT: the real database has
// committed, while the actual driver's Tx.Commit cannot acknowledge success.
// It never changes SQL, row results, authentication or transaction outcomes.
type mysqlCommitReplyProxy struct {
	listener    net.Listener
	upstream    string
	arm         atomic.Bool
	dropped     atomic.Bool
	lockArm     atomic.Bool
	lockDropped atomic.Bool
	mu          sync.Mutex
	connections map[net.Conn]bool
	closed      bool
	wg          sync.WaitGroup
}

func readMySQLPacket(conn net.Conn) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if size == 0 {
		return header, nil
	}
	packet := make([]byte, 4+size)
	copy(packet, header)
	_, err := io.ReadFull(conn, packet[4:])
	return packet, err
}

func writeMySQLPacket(conn net.Conn, packet []byte) error {
	for len(packet) > 0 {
		n, err := conn.Write(packet)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		packet = packet[n:]
	}
	return nil
}

func (p *mysqlCommitReplyProxy) add(conn net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		conn.Close()
		return false
	}
	p.connections[conn] = true
	return true
}

func newMySQLCommitReplyProxy(t *testing.T, dsn string) (*mysqlCommitReplyProxy, string) {
	t.Helper()
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid generated fixture connection")
	}
	if config.Net != "tcp" || config.TLS != nil || config.TLSConfig != "" {
		t.Skip("protocol acknowledgement fixture requires an explicitly isolated plaintext TCP MySQL channel; TLS is never downgraded")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &mysqlCommitReplyProxy{listener: listener, upstream: config.Addr, connections: map[net.Conn]bool{}}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			if !p.add(client) {
				return
			}
			p.wg.Add(1)
			go p.forward(client)
		}
	}()
	t.Cleanup(p.close)
	config.Addr = listener.Addr().String()
	return p, config.FormatDSN()
}

func (p *mysqlCommitReplyProxy) forward(client net.Conn) {
	defer p.wg.Done()
	defer client.Close()
	server, err := net.DialTimeout("tcp", p.upstream, 5*time.Second)
	if err != nil {
		return
	}
	if !p.add(server) {
		return
	}
	defer server.Close()
	var dropResponse atomic.Bool
	var lockPrepare atomic.Bool
	var lockStatement atomic.Uint32
	var dropLockRow atomic.Bool
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer client.Close()
		defer server.Close()
		for {
			packet, err := readMySQLPacket(client)
			if err != nil {
				return
			}
			if len(packet) > 5 && packet[3] == 0 && packet[4] == 3 && strings.EqualFold(strings.TrimSpace(string(packet[5:])), "COMMIT") && p.arm.CompareAndSwap(true, false) {
				dropResponse.Store(true)
			}
			if len(packet) > 5 && packet[3] == 0 && packet[4] == 22 && p.lockArm.Load() && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(string(packet[5:]))), "SELECT GET_LOCK(") {
				lockPrepare.Store(true)
			}
			if len(packet) >= 9 && packet[3] == 0 && packet[4] == 23 && lockStatement.Load() != 0 && binary.LittleEndian.Uint32(packet[5:9]) == lockStatement.Load() && p.lockArm.CompareAndSwap(true, false) {
				dropLockRow.Store(true)
			}
			if err := writeMySQLPacket(server, packet); err != nil {
				return
			}
		}
	}()
	for {
		packet, err := readMySQLPacket(server)
		if err != nil {
			break
		}
		if lockPrepare.Load() && len(packet) >= 9 && packet[4] == 0 {
			lockStatement.Store(binary.LittleEndian.Uint32(packet[5:9]))
			lockPrepare.Store(false)
		}
		// Prepared SELECT returns a binary row: header 0, one-byte null
		// bitmap and the integer's fixed-width little-endian value. The
		// server has actually acquired the lock when that value is one.
		if dropLockRow.Load() && mysqlBinaryOne(packet) {
			p.lockDropped.Store(true)
			break
		}
		if dropResponse.Load() {
			if len(packet) > 4 && packet[4] == 0 {
				p.dropped.Store(true)
			}
			break
		}
		if err := writeMySQLPacket(client, packet); err != nil {
			break
		}
	}
	client.Close()
	server.Close()
	<-readDone
}

func mysqlBinaryOne(packet []byte) bool {
	if len(packet) != 7 && len(packet) != 8 && len(packet) != 10 && len(packet) != 14 {
		return false
	}
	if packet[4] != 0 || packet[5] != 0 || packet[6] != 1 {
		return false
	}
	for _, value := range packet[7:] {
		if value != 0 {
			return false
		}
	}
	return true
}

func TestMySQLAcquiredSchemaLockReplyLossDiscardsUnknownSession(t *testing.T) {
	f := mysqlStore(t, true)
	proxy, dsn := newMySQLCommitReplyProxy(t, f.dsn)
	uncertain, err := store.OpenMySQL(f.ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { uncertain.DB.Close() })
	uncertain.DB.SetMaxOpenConns(1)
	var original uint64
	if err := uncertain.DB.QueryRowContext(f.ctx, `SELECT CONNECTION_ID()`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	proxy.lockArm.Store(true)
	if err := uncertain.VerifyMySQLSchema(f.ctx); !errors.Is(err, ownermigration.ErrOutcomeUnknown) {
		t.Fatalf("unacknowledged lock acquisition was not unknown: %v", err)
	}
	if !proxy.lockDropped.Load() {
		t.Fatal("fault did not observe the actual GET_LOCK=1 result")
	}
	if err := uncertain.VerifyMySQLSchema(f.ctx); err != nil {
		t.Fatalf("fresh executor could not verify after lock acknowledgement loss: %v", err)
	}
	var current uint64
	if err := uncertain.DB.QueryRowContext(f.ctx, `SELECT CONNECTION_ID()`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current == original {
		t.Fatal("unknown lock session returned to the pool")
	}
}

func (p *mysqlCommitReplyProxy) close() {
	p.mu.Lock()
	p.closed = true
	p.listener.Close()
	for conn := range p.connections {
		conn.Close()
	}
	p.mu.Unlock()
	p.wg.Wait()
}

func TestMySQLCommittedCreateReplyLossQueriesOriginalKey(t *testing.T) {
	f := mysqlStore(t, true)
	proxy, dsn := newMySQLCommitReplyProxy(t, f.dsn)
	uncertain, err := store.OpenMySQL(f.ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { uncertain.DB.Close() })
	uncertain.DB.SetMaxOpenConns(1)
	op := testOperation(t, 2)
	proxy.arm.Store(true)
	if _, err := uncertain.Create(f.ctx, op); err == nil {
		t.Fatal("lost COMMIT reply reported creation success")
	}
	if !proxy.dropped.Load() {
		t.Fatal("fault did not reach the actual successful COMMIT reply")
	}
	observed, err := f.store.Get(f.ctx, op.TenantID, op.SubjectID, op.RequestKey)
	if err != nil || observed.ID != op.ID || observed.State != app.Queued {
		t.Fatalf("original-key recovery lost committed operation: %#v %v", observed, err)
	}
	duplicate := op
	duplicate.ID = fixtureID(t)
	replayed, err := f.store.Create(f.ctx, duplicate)
	if err != nil || replayed.ID != op.ID {
		t.Fatalf("same key created another instance after unknown commit: %#v %v", replayed, err)
	}
	duplicate.RequestDigest = strings.Repeat("d", 64)
	if _, err := f.store.Create(f.ctx, duplicate); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("changed request reused original commit: %v", err)
	}
	var count int
	if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM market_install_operations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("unknown commit duplicated installation")
	}
}

func TestMySQLCommittedConfirmReplyLossIsUnknownUntilQuery(t *testing.T) {
	f := mysqlStore(t, true)
	op := testOperation(t, 1)
	work := f.dispatched(t, op)
	proxy, dsn := newMySQLCommitReplyProxy(t, f.dsn)
	uncertain, err := store.OpenMySQL(f.ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { uncertain.DB.Close() })
	uncertain.DB.SetMaxOpenConns(1)
	receipt := app.Receipt{Owner: work.Step.Owner, ResourceID: "existing-owner-resource", PayloadHash: work.PayloadHash, Data: json.RawMessage(`{"committed":true}`)}
	proxy.arm.Store(true)
	if err := uncertain.Confirm(f.ctx, work, receipt); err == nil {
		t.Fatal("lost final COMMIT reply reported completion")
	}
	if !proxy.dropped.Load() {
		t.Fatal("fault did not reach the actual successful COMMIT reply")
	}
	observed, err := f.store.Get(f.ctx, op.TenantID, op.SubjectID, op.RequestKey)
	if err != nil || observed.State != app.Completed {
		t.Fatalf("committed result is not observable by original key: %#v %v", observed, err)
	}
	if next, err := f.store.Claim(f.ctx, 60); err != nil || next != nil {
		t.Fatalf("completed unknown reply was executed again: %#v %v", next, err)
	}
	if err := f.store.Confirm(f.ctx, work, receipt); !errors.Is(err, app.ErrLeaseLost) {
		t.Fatalf("stale final work was reused: %v", err)
	}
}
