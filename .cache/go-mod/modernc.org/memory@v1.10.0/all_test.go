// Copyright 2017 The Memory Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memory // import "modernc.org/memory"

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"modernc.org/mathutil"
)

func caller(s string, va ...interface{}) {
	if s == "" {
		s = strings.Repeat("%v ", len(va))
	}
	_, fn, fl, _ := runtime.Caller(2)
	fmt.Fprintf(os.Stderr, "# caller: %s:%d: ", path.Base(fn), fl)
	fmt.Fprintf(os.Stderr, s, va...)
	fmt.Fprintln(os.Stderr)
	_, fn, fl, _ = runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "# \tcallee: %s:%d: ", path.Base(fn), fl)
	fmt.Fprintln(os.Stderr)
	os.Stderr.Sync()
}

func dbg(s string, va ...interface{}) {
	if s == "" {
		s = strings.Repeat("%v ", len(va))
	}
	_, fn, fl, _ := runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "# dbg %s:%d: ", path.Base(fn), fl)
	fmt.Fprintf(os.Stderr, s, va...)
	fmt.Fprintln(os.Stderr)
	os.Stderr.Sync()
}

func TODO(...interface{}) string { //TODOOK
	_, fn, fl, _ := runtime.Caller(1)
	return fmt.Sprintf("# TODO: %s:%d:\n", path.Base(fn), fl) //TODOOK
}

func use(...interface{}) {}

func init() {
	use(caller, dbg, TODO) //TODOOK
}

// ============================================================================

const quota = 128 << 20

var (
	max    = 2 * osPageSize
	bigMax = 2 * pageSize
)

type block struct {
	p    uintptr
	size int
}

func test1u(t *testing.T, max int) {
	var alloc Allocator

	defer alloc.Close()

	rem := quota
	var a []block
	srng, err := mathutil.NewFC32(0, math.MaxInt32, true)
	if err != nil {
		t.Fatal(err)
	}

	vrng, err := mathutil.NewFC32(0, math.MaxInt32, true)
	if err != nil {
		t.Fatal(err)
	}

	// Allocate
	for rem > 0 {
		size := srng.Next()%max + 1
		rem -= size
		p, err := alloc.UintptrMalloc(size)
		if err != nil {
			t.Fatal(err)
		}

		a = append(a, block{p, size})
		for i := 0; i < size; i++ {
			*(*byte)(unsafe.Pointer(p + uintptr(i))) = byte(vrng.Next())
		}
	}
	if counters {
		t.Logf("allocs %v, mmaps %v, bytes %v, overhead %v (%.2f%%).", alloc.Allocs, alloc.Mmaps, alloc.Bytes, alloc.Bytes-quota, 100*float64(alloc.Bytes-quota)/quota)
	}
	srng.Seek(0)
	vrng.Seek(0)
	// Verify
	for i, b := range a {
		if g, e := b.size, srng.Next()%max+1; g != e {
			t.Fatal(i, g, e)
		}

		if a, b := b.size, UintptrUsableSize(b.p); a > b {
			t.Fatal(i, a, b)
		}

		for j := 0; j < b.size; j++ {
			g := *(*byte)(unsafe.Pointer(b.p + uintptr(j)))
			if e := byte(vrng.Next()); g != e {
				t.Fatalf("%v,%v %#x: %#02x %#02x", i, j, b.p+uintptr(j), g, e)
			}

			*(*byte)(unsafe.Pointer(b.p + uintptr(j))) = 0
		}
	}
	// Shuffle
	for i := range a {
		j := srng.Next() % len(a)
		a[i], a[j] = a[j], a[i]
	}
	// Free
	for _, b := range a {
		if err := alloc.UintptrFree(b.p); err != nil {
			t.Fatal(err)
		}
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func Test1USmall(t *testing.T) { test1u(t, max) }
func Test1UBig(t *testing.T)   { test1u(t, bigMax) }

func test2u(t *testing.T, max int) {
	var alloc Allocator

	defer alloc.Close()

	rem := quota
	var a []block
	srng, err := mathutil.NewFC32(0, math.MaxInt32, true)
	if err != nil {
		t.Fatal(err)
	}

	vrng, err := mathutil.NewFC32(0, math.MaxInt32, true)
	if err != nil {
		t.Fatal(err)
	}

	// Allocate
	for rem > 0 {
		size := srng.Next()%max + 1
		rem -= size
		p, err := alloc.UintptrMalloc(size)
		if err != nil {
			t.Fatal(err)
		}

		a = append(a, block{p, size})
		for i := 0; i < size; i++ {
			*(*byte)(unsafe.Pointer(p + uintptr(i))) = byte(vrng.Next())
		}
	}
	if counters {
		t.Logf("allocs %v, mmaps %v, bytes %v, overhead %v (%.2f%%).", alloc.Allocs, alloc.Mmaps, alloc.Bytes, alloc.Bytes-quota, 100*float64(alloc.Bytes-quota)/quota)
	}
	srng.Seek(0)
	vrng.Seek(0)
	// Verify & free
	for i, b := range a {
		if g, e := b.size, srng.Next()%max+1; g != e {
			t.Fatal(i, g, e)
		}

		if a, b := b.size, UintptrUsableSize(b.p); a > b {
			t.Fatal(i, a, b)
		}

		for j := 0; j < b.size; j++ {
			g := *(*byte)(unsafe.Pointer(b.p + uintptr(j)))
			if e := byte(vrng.Next()); g != e {
				t.Fatalf("%v,%v %#x: %#02x %#02x", i, j, b.p+uintptr(j), g, e)
			}

			*(*byte)(unsafe.Pointer(b.p + uintptr(j))) = 0
		}
		if err := alloc.UintptrFree(b.p); err != nil {
			t.Fatal(err)
		}
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func Test2USmall(t *testing.T) { test2u(t, max) }
func Test2UBig(t *testing.T)   { test2u(t, bigMax) }

func test3u(t *testing.T, max int) {
	var alloc Allocator

	defer alloc.Close()

	rem := quota
	m := map[block][]byte{}
	srng, err := mathutil.NewFC32(1, max, true)
	if err != nil {
		t.Fatal(err)
	}

	vrng, err := mathutil.NewFC32(1, max, true)
	if err != nil {
		t.Fatal(err)
	}

	for rem > 0 {
		switch srng.Next() % 3 {
		case 0, 1: // 2/3 allocate
			size := srng.Next()
			rem -= size
			p, err := alloc.UintptrMalloc(size)
			if err != nil {
				t.Fatal(err)
			}

			b := make([]byte, size)
			for i := range b {
				b[i] = byte(vrng.Next())
				*(*byte)(unsafe.Pointer(p + uintptr(i))) = b[i]
			}
			m[block{p, size}] = append([]byte(nil), b...)
		default: // 1/3 free
			for b, v := range m {
				for i, v := range v {
					if *(*byte)(unsafe.Pointer(b.p + uintptr(i))) != v {
						t.Fatal("corrupted heap")
					}
				}

				if a, b := b.size, UintptrUsableSize(b.p); a > b {
					t.Fatal(a, b)
				}

				for j := 0; j < b.size; j++ {
					*(*byte)(unsafe.Pointer(b.p + uintptr(j))) = 0
				}
				rem += b.size
				alloc.UintptrFree(b.p)
				delete(m, b)
				break
			}
		}
	}
	if counters {
		t.Logf("allocs %v, mmaps %v, bytes %v, overhead %v (%.2f%%).", alloc.Allocs, alloc.Mmaps, alloc.Bytes, alloc.Bytes-quota, 100*float64(alloc.Bytes-quota)/quota)
	}
	for b, v := range m {
		for i, v := range v {
			if *(*byte)(unsafe.Pointer(b.p + uintptr(i))) != v {
				t.Fatal("corrupted heap")
			}
		}

		if a, b := b.size, UintptrUsableSize(b.p); a > b {
			t.Fatal(a, b)
		}

		for j := 0; j < b.size; j++ {
			*(*byte)(unsafe.Pointer(b.p + uintptr(j))) = 0
		}
		alloc.UintptrFree(b.p)
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func Test3USmall(t *testing.T) { test3u(t, max) }
func Test3UBig(t *testing.T)   { test3u(t, bigMax) }

func TestUFree(t *testing.T) {
	var alloc Allocator

	defer alloc.Close()

	p, err := alloc.UintptrMalloc(1)
	if err != nil {
		t.Fatal(err)
	}

	if err := alloc.UintptrFree(p); err != nil {
		t.Fatal(err)
	}

	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func TestUMalloc(t *testing.T) {
	var alloc Allocator

	defer alloc.Close()

	p, err := alloc.UintptrMalloc(maxSlotSize)
	if err != nil {
		t.Fatal(err)
	}

	pg := (*page)(unsafe.Pointer(p &^ uintptr(osPageMask)))
	if 1<<pg.log > maxSlotSize {
		t.Fatal(1<<pg.log, maxSlotSize)
	}

	if err := alloc.UintptrFree(p); err != nil {
		t.Fatal(err)
	}

	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func test1(t *testing.T, max int) {
	var alloc Allocator

	defer alloc.Close()

	rem := quota
	var a [][]byte
	srng, err := mathutil.NewFC32(0, math.MaxInt32, true)
	if err != nil {
		t.Fatal(err)
	}

	vrng, err := mathutil.NewFC32(0, math.MaxInt32, true)
	if err != nil {
		t.Fatal(err)
	}

	// Allocate
	for rem > 0 {
		size := srng.Next()%max + 1
		rem -= size
		b, err := alloc.Malloc(size)
		if err != nil {
			t.Fatal(err)
		}

		a = append(a, b)
		for i := range b {
			b[i] = byte(vrng.Next())
		}
	}
	if counters {
		t.Logf("allocs %v, mmaps %v, bytes %v, overhead %v (%.2f%%).", alloc.Allocs, alloc.Mmaps, alloc.Bytes, alloc.Bytes-quota, 100*float64(alloc.Bytes-quota)/quota)
	}
	srng.Seek(0)
	vrng.Seek(0)
	// Verify
	for i, b := range a {
		if g, e := len(b), srng.Next()%max+1; g != e {
			t.Fatal(i, g, e)
		}

		if a, b := len(b), UsableSize(&b[0]); a > b {
			t.Fatal(i, a, b)
		}

		for i, g := range b {
			if e := byte(vrng.Next()); g != e {
				t.Fatalf("%v %p: %#02x %#02x", i, &b[i], g, e)
			}

			b[i] = 0
		}
	}
	// Shuffle
	for i := range a {
		j := srng.Next() % len(a)
		a[i], a[j] = a[j], a[i]
	}
	// Free
	for _, b := range a {
		if err := alloc.Free(b); err != nil {
			t.Fatal(err)
		}
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func Test1Small(t *testing.T) { test1(t, max) }
func Test1Big(t *testing.T)   { test1(t, bigMax) }

func test2(t *testing.T, max int) {
	var alloc Allocator

	defer alloc.Close()

	rem := quota
	var a [][]byte
	srng, err := mathutil.NewFC32(0, math.MaxInt32, true)
	if err != nil {
		t.Fatal(err)
	}

	vrng, err := mathutil.NewFC32(0, math.MaxInt32, true)
	if err != nil {
		t.Fatal(err)
	}

	// Allocate
	for rem > 0 {
		size := srng.Next()%max + 1
		rem -= size
		b, err := alloc.Malloc(size)
		if err != nil {
			t.Fatal(err)
		}

		a = append(a, b)
		for i := range b {
			b[i] = byte(vrng.Next())
		}
	}
	if counters {
		t.Logf("allocs %v, mmaps %v, bytes %v, overhead %v (%.2f%%).", alloc.Allocs, alloc.Mmaps, alloc.Bytes, alloc.Bytes-quota, 100*float64(alloc.Bytes-quota)/quota)
	}
	srng.Seek(0)
	vrng.Seek(0)
	// Verify & free
	for i, b := range a {
		if g, e := len(b), srng.Next()%max+1; g != e {
			t.Fatal(i, g, e)
		}

		if a, b := len(b), UsableSize(&b[0]); a > b {
			t.Fatal(i, a, b)
		}

		for i, g := range b {
			if e := byte(vrng.Next()); g != e {
				t.Fatalf("%v %p: %#02x %#02x", i, &b[i], g, e)
			}

			b[i] = 0
		}
		if err := alloc.Free(b); err != nil {
			t.Fatal(err)
		}
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func Test2Small(t *testing.T) { test2(t, max) }
func Test2Big(t *testing.T)   { test2(t, bigMax) }

func test3(t *testing.T, max int) {
	var alloc Allocator

	defer alloc.Close()

	rem := quota
	m := map[*[]byte][]byte{}
	srng, err := mathutil.NewFC32(1, max, true)
	if err != nil {
		t.Fatal(err)
	}

	vrng, err := mathutil.NewFC32(1, max, true)
	if err != nil {
		t.Fatal(err)
	}

	for rem > 0 {
		switch srng.Next() % 3 {
		case 0, 1: // 2/3 allocate
			size := srng.Next()
			rem -= size
			b, err := alloc.Malloc(size)
			if err != nil {
				t.Fatal(err)
			}

			for i := range b {
				b[i] = byte(vrng.Next())
			}
			m[&b] = append([]byte(nil), b...)
		default: // 1/3 free
			for k, v := range m {
				b := *k
				if !bytes.Equal(b, v) {
					t.Fatal("corrupted heap")
				}

				if a, b := len(b), UsableSize(&b[0]); a > b {
					t.Fatal(a, b)
				}

				for i := range b {
					b[i] = 0
				}
				rem += len(b)
				alloc.Free(b)
				delete(m, k)
				break
			}
		}
	}
	if counters {
		t.Logf("allocs %v, mmaps %v, bytes %v, overhead %v (%.2f%%).", alloc.Allocs, alloc.Mmaps, alloc.Bytes, alloc.Bytes-quota, 100*float64(alloc.Bytes-quota)/quota)
	}
	for k, v := range m {
		b := *k
		if !bytes.Equal(b, v) {
			t.Fatal("corrupted heap")
		}

		if a, b := len(b), UsableSize(&b[0]); a > b {
			t.Fatal(a, b)
		}

		for i := range b {
			b[i] = 0
		}
		alloc.Free(b)
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func Test3Small(t *testing.T) { test3(t, max) }
func Test3Big(t *testing.T)   { test3(t, bigMax) }

func TestFree(t *testing.T) {
	var alloc Allocator

	defer alloc.Close()

	b, err := alloc.Malloc(1)
	if err != nil {
		t.Fatal(err)
	}

	if err := alloc.Free(b[:0]); err != nil {
		t.Fatal(err)
	}

	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func TestMalloc(t *testing.T) {
	var alloc Allocator

	defer alloc.Close()

	b, err := alloc.Malloc(maxSlotSize)
	if err != nil {
		t.Fatal(err)
	}

	p := (*page)(unsafe.Pointer(uintptr(unsafe.Pointer(&b[0])) &^ uintptr(osPageMask)))
	if 1<<p.log > maxSlotSize {
		t.Fatal(1<<p.log, maxSlotSize)
	}

	if err := alloc.Free(b[:0]); err != nil {
		t.Fatal(err)
	}

	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		t.Fatalf("%+v", alloc)
	}
}

func benchmarkFree(b *testing.B, size int) {
	var alloc Allocator

	defer alloc.Close()

	a := make([][]byte, b.N)
	for i := range a {
		p, err := alloc.Malloc(size)
		if err != nil {
			b.Fatal(err)
		}

		a[i] = p
	}
	b.ResetTimer()
	for _, b := range a {
		alloc.Free(b)
	}
	b.StopTimer()
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		b.Fatalf("%+v", alloc)
	}
}

func BenchmarkFree16(b *testing.B) { benchmarkFree(b, 1<<4) }
func BenchmarkFree32(b *testing.B) { benchmarkFree(b, 1<<5) }
func BenchmarkFree64(b *testing.B) { benchmarkFree(b, 1<<6) }

func benchmarkCalloc(b *testing.B, size int) {
	var alloc Allocator

	defer alloc.Close()

	a := make([][]byte, b.N)
	b.ResetTimer()
	for i := range a {
		p, err := alloc.Calloc(size)
		if err != nil {
			b.Fatal(err)
		}

		a[i] = p
	}
	b.StopTimer()
	for _, b := range a {
		alloc.Free(b)
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		b.Fatalf("%+v", alloc)
	}
}

func BenchmarkCalloc16(b *testing.B) { benchmarkCalloc(b, 1<<4) }
func BenchmarkCalloc32(b *testing.B) { benchmarkCalloc(b, 1<<5) }
func BenchmarkCalloc64(b *testing.B) { benchmarkCalloc(b, 1<<6) }

func benchmarkGoCalloc(b *testing.B, size int) {
	a := make([][]byte, b.N)
	b.ResetTimer()
	for i := range a {
		a[i] = make([]byte, size)
	}
	b.StopTimer()
	use(a)
}

func BenchmarkGoCalloc16(b *testing.B) { benchmarkGoCalloc(b, 1<<4) }
func BenchmarkGoCalloc32(b *testing.B) { benchmarkGoCalloc(b, 1<<5) }
func BenchmarkGoCalloc64(b *testing.B) { benchmarkGoCalloc(b, 1<<6) }

func benchmarkMalloc(b *testing.B, size int) {
	var alloc Allocator

	defer alloc.Close()

	a := make([][]byte, b.N)
	b.ResetTimer()
	for i := range a {
		p, err := alloc.Malloc(size)
		if err != nil {
			b.Fatal(err)
		}

		a[i] = p
	}
	b.StopTimer()
	for _, b := range a {
		alloc.Free(b)
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		b.Fatalf("%+v", alloc)
	}
}

func BenchmarkMalloc16(b *testing.B) { benchmarkMalloc(b, 1<<4) }
func BenchmarkMalloc32(b *testing.B) { benchmarkMalloc(b, 1<<5) }
func BenchmarkMalloc64(b *testing.B) { benchmarkMalloc(b, 1<<6) }

func benchmarkUintptrFree(b *testing.B, size int) {
	var alloc Allocator

	defer alloc.Close()

	a := make([]uintptr, b.N)
	for i := range a {
		p, err := alloc.UintptrMalloc(size)
		if err != nil {
			b.Fatal(err)
		}

		a[i] = p
	}
	b.ResetTimer()
	for _, p := range a {
		alloc.UintptrFree(p)
	}
	b.StopTimer()
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		b.Fatalf("%+v", alloc)
	}
}

func BenchmarkUintptrFree16(b *testing.B) { benchmarkUintptrFree(b, 1<<4) }
func BenchmarkUintptrFree32(b *testing.B) { benchmarkUintptrFree(b, 1<<5) }
func BenchmarkUintptrFree64(b *testing.B) { benchmarkUintptrFree(b, 1<<6) }

func benchmarkUintptrCalloc(b *testing.B, size int) {
	var alloc Allocator

	defer alloc.Close()

	a := make([]uintptr, b.N)
	b.ResetTimer()
	for i := range a {
		p, err := alloc.UintptrCalloc(size)
		if err != nil {
			b.Fatal(err)
		}

		a[i] = p
	}
	b.StopTimer()
	for _, p := range a {
		alloc.UintptrFree(p)
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		b.Fatalf("%+v", alloc)
	}
}

func BenchmarkUintptrCalloc16(b *testing.B) { benchmarkUintptrCalloc(b, 1<<4) }
func BenchmarkUintptrCalloc32(b *testing.B) { benchmarkUintptrCalloc(b, 1<<5) }
func BenchmarkUintptrCalloc64(b *testing.B) { benchmarkUintptrCalloc(b, 1<<6) }

func benchmarkUintptrMalloc(b *testing.B, size int) {
	var alloc Allocator

	defer alloc.Close()

	a := make([]uintptr, b.N)
	b.ResetTimer()
	for i := range a {
		p, err := alloc.UintptrMalloc(size)
		if err != nil {
			b.Fatal(err)
		}

		a[i] = p
	}
	b.StopTimer()
	for _, p := range a {
		alloc.UintptrFree(p)
	}
	if alloc.Allocs != 0 || alloc.Mmaps != 0 || alloc.Bytes != 0 || len(alloc.regs) != 0 {
		b.Fatalf("%+v", alloc)
	}
}

func BenchmarkUintptrMalloc16(b *testing.B) { benchmarkUintptrMalloc(b, 1<<4) }
func BenchmarkUintptrMalloc32(b *testing.B) { benchmarkUintptrMalloc(b, 1<<5) }
func BenchmarkUintptrMalloc64(b *testing.B) { benchmarkUintptrMalloc(b, 1<<6) }
