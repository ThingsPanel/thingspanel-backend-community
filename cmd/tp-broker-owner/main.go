package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"project/internal/brokercontrol"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(); err != nil {
		// Error details can contain DSNs or credentials; keep startup failures generic.
		_, _ = io.WriteString(os.Stderr, "broker owner startup failed\n")
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("TP_BROKER_OWNER_DATABASE_DSN")
	tokenFile := os.Getenv("TP_BROKER_OWNER_TOKENS_FILE")
	serviceFile := os.Getenv("TP_BROKER_OWNER_SERVICES_FILE")
	listen := os.Getenv("TP_BROKER_OWNER_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:9801"
	}
	if dsn == "" || tokenFile == "" {
		return errors.New("configuration required")
	}
	tokens, err := readJSONFile[[]brokercontrol.TokenBinding](tokenFile, 64<<10)
	if err != nil {
		return err
	}
	services := []brokercontrol.ServiceCredential{}
	if serviceFile != "" {
		services, err = readJSONFile[[]brokercontrol.ServiceCredential](serviceFile, 1<<20)
		if err != nil {
			return err
		}
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	owner, err := brokercontrol.NewOwner(brokercontrol.NewGormRepository(db), brokercontrol.Config{Tokens: tokens, Services: services, GrantTTL: brokercontrol.DefaultGrantTTL})
	if err != nil {
		_ = sqlDB.Close()
		return err
	}
	server := &http.Server{Addr: listen, Handler: owner.RuntimeHandler(brokercontrol.NewMigrationStore(db)), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	cert, key := os.Getenv("TP_BROKER_OWNER_TLS_CERT"), os.Getenv("TP_BROKER_OWNER_TLS_KEY")
	if (cert == "") != (key == "") {
		_ = sqlDB.Close()
		return errors.New("incomplete TLS configuration")
	}
	if !isLoopback(listen) && cert == "" {
		_ = sqlDB.Close()
		return errors.New("non-loopback listener requires TLS")
	}
	if cert != "" {
		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() {
		if cert != "" {
			serveErr <- server.ListenAndServeTLS(cert, key)
		} else {
			serveErr <- server.ListenAndServe()
		}
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = sqlDB.Close()
		return nil
	case err := <-serveErr:
		_ = sqlDB.Close()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func readJSONFile[T any](path string, maxBytes int64) (T, error) {
	var value T
	f, err := os.Open(path)
	if err != nil {
		return value, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return value, err
	}
	if int64(len(data)) > maxBytes {
		return value, errors.New("configuration file too large")
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&value); err != nil {
		return value, errors.New("invalid configuration file")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return value, errors.New("invalid configuration file")
	}
	return value, nil
}

func isLoopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
