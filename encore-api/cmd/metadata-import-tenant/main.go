// metadata-import-tenant performs a verified tenant metadata import. It never
// connects to or changes the legacy database. Credentials use PG* environment.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"encore.app/devicemetadata/offlineupgrade"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func readPrivateFile(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, fmt.Errorf("private archive unavailable")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if e != nil || len(b) > 64<<20 {
		return nil, fmt.Errorf("private archive unreadable or too large")
	}
	return b, nil
}

func run() error {
	apply := flag.Bool("apply", false, "commit the verified tenant import (default validates only)")
	source := flag.String("source-archive", "", "private immutable source archive")
	target := flag.String("target-archive", "", "private pre-import native backup")
	resolutions := flag.String("resolutions", "", "reviewed original parent/name resolutions file")
	flag.Parse()
	var in offlineupgrade.TenantImportInput
	d := json.NewDecoder(io.LimitReader(os.Stdin, 64<<20))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("invalid private import input")
	}
	p, e := offlineupgrade.PlanTenantImport(in)
	if e != nil {
		return e
	}
	src, e := readPrivateFile(*source)
	if e != nil {
		return e
	}
	dst, e := readPrivateFile(*target)
	if e != nil {
		return e
	}
	approved, e := readPrivateFile(*resolutions)
	if e != nil {
		return e
	}
	if e := offlineupgrade.ValidateTenantImportArchives(in, src, dst, approved); e != nil {
		return e
	}
	if !*apply {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"state": "validated-not-applied", "tenant_id": p.TenantID, "input_sha256": p.Digest, "groups": len(p.Groups), "devices": len(p.Devices), "members": len(p.Members), "gateways": len(p.Gateways), "runtime_ownership_changed": false})
	}
	if os.Getenv("PGDATABASE") != in.TargetDatabase {
		return fmt.Errorf("PGDATABASE must exactly match verified target")
	}
	db, e := gorm.Open(postgres.Open(""), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		return fmt.Errorf("native target connection failed")
	}
	pool, e := db.DB()
	if e != nil {
		return fmt.Errorf("native pool unavailable")
	}
	defer pool.Close()
	pool.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	r, e := offlineupgrade.ApplyTenantImportPG(ctx, db, in)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(r)
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
