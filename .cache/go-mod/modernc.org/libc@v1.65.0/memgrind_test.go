// Copyright 2021 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !libc.membrk && libc.memgrind && !(linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64 || 386 || arm))

package libc // import "modernc.org/libc"

import (
	"math/rand"
	"testing"
)

func TestTLSAllocator(t *testing.T) {
	const rq = int(stackSegmentSize*3) / 4
	MemAuditStart()
	tls := NewTLS()
	for i := 0; i < 10; i++ {
		tls.Alloc(rq)
	}
	for i := 0; i < 10; i++ {
		tls.Free(rq)
	}
	tls.Close()
	if err := MemAuditReport(); err != nil {
		t.Fatal(err)
	}
}

func TestTLSAllocator2(t *testing.T) {
	MemAuditStart()
	tls := NewTLS()
	for rq := 1; rq < 1000; rq++ {
		tls.Alloc(rq)
	}
	for rq := 999; rq > 0; rq-- {
		tls.Free(rq)
	}
	tls.Close()
	if err := MemAuditReport(); err != nil {
		t.Fatal(err)
	}
}

func TestTLSAllocator3(t *testing.T) {
	a := make([]int, 1000)
	r := rand.New(rand.NewSource(42))
	for i := range a {
		a[i] = int(r.Int31n(2 * int32(stackSegmentSize)))
	}
	MemAuditStart()
	tls := NewTLS()
	for _, rq := range a {
		tls.Alloc(rq)
	}
	for i := len(a) - 1; i >= 0; i-- {
		tls.Free(a[i])
	}
	tls.Close()
	if err := MemAuditReport(); err != nil {
		t.Fatal(err)
	}
}
