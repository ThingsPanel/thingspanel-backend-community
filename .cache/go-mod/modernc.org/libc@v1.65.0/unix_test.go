// Copyright 2023 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix
// +build unix

package libc // import "modernc.org/libc"

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// https://gitlab.com/cznic/libc/-/issues/29
func TestIssue29(t *testing.T) {
	dir := t.TempDir()

	fn := filepath.Join(dir, "test")
	if err := os.WriteFile(fn, make([]byte, 1<<20), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(fn, os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()

	tls := NewTLS()
	defer tls.Close()
	d := Xmmap(tls, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED, int32(f.Fd()), 0)
	if d == 0 {
		t.Fatal("mmap failed")
	}

	t.Logf("%#0x", d)
	if rc := Xmunmap(tls, d, 4096); rc != 0 {
		t.Fatalf("munmap failed: %v", rc)
	}
}
