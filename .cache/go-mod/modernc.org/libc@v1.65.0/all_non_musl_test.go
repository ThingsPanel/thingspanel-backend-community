// Copyright 2020 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !(linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64 || 386 || arm))

package libc // import "modernc.org/libc"

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	_ "modernc.org/cc/v4"
	_ "modernc.org/ccgo/v4/lib"
	_ "modernc.org/fileutil/ccgo"
	ctime "modernc.org/libc/time"
)

func TestXfmod(t *testing.T) {
	x := 1.3518643030646695
	y := 6.283185307179586
	if g, e := Xfmod(nil, x, y), 1.3518643030646695; g != e {
		t.Fatal(g, e)
	}
}

func TestSwap(t *testing.T) {
	if g, e := X__builtin_bswap16(nil, 0x1234), uint16(0x3412); g != e {
		t.Errorf("%#04x %#04x", g, e)
	}
	if g, e := X__builtin_bswap32(nil, 0x12345678), uint32(0x78563412); g != e {
		t.Errorf("%#04x %#04x", g, e)
	}
	if g, e := X__builtin_bswap64(nil, 0x123456789abcdef0), uint64(0xf0debc9a78563412); g != e {
		t.Errorf("%#04x %#04x", g, e)
	}
}

var (
	valist       [256]byte
	formatString [256]byte
	srcString    [256]byte
	testPrintfS1 = [...]byte{'X', 'Y', 0}
)

func TestPrintf(t *testing.T) {
	isWindows = true
	i := uint64(0x123456789abcdef)
	j := uint64(0xf123456789abcde)
	k := uint64(0x23456789abcdef1)
	l := uint64(0xef123456789abcd)
	for itest, test := range []struct {
		fmt    string
		args   []interface{}
		result string
	}{
		{
			"%I64x %I32x %I64x %I32x",
			[]interface{}{int64(i), int32(j), int64(k), int32(l)},
			"123456789abcdef 789abcde 23456789abcdef1 6789abcd",
		},
		{
			"%llx %x %llx %x",
			[]interface{}{int64(i), int32(j), int64(k), int32(l)},
			"123456789abcdef 789abcde 23456789abcdef1 6789abcd",
		},
		{
			"%.1s\n",
			[]interface{}{uintptr(unsafe.Pointer(&testPrintfS1[0]))},
			"X\n",
		},
		{
			"%.2s\n",
			[]interface{}{uintptr(unsafe.Pointer(&testPrintfS1[0]))},
			"XY\n",
		},
	} {
		copy(formatString[:], test.fmt+"\x00")
		b := printf(uintptr(unsafe.Pointer(&formatString[0])), VaList(uintptr(unsafe.Pointer(&valist[0])), test.args...))
		if g, e := string(b), test.result; g != e {
			t.Errorf("%v: %q %q", itest, g, e)
		}
	}
}

func TestStrtod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TODO")
	}

	tls := NewTLS()
	defer tls.Close()

	for itest, test := range []struct {
		s      string
		result float64
	}{
		{"+0", 0},
		{"+1", 1},
		{"+2", 2},
		{"-0", 0},
		{"-1", -1},
		{"-2", -2},
		{".5", .5},
		{"0", 0},
		{"1", 1},
		{"1.", 1},
		{"1.024e3", 1024},
		{"16", 16},
		{"2", 2},
		{"32", 32},
	} {
		copy(srcString[:], test.s+"\x00")
		if g, e := Xstrtod(tls, uintptr(unsafe.Pointer(&srcString[0])), 0), test.result; g != e {
			t.Errorf("%v: %q: %v %v", itest, test.s, g, e)
		}
	}
}

func TestParseZone(t *testing.T) {
	for itest, test := range []struct {
		in, out string
		off     int
	}{
		{"America/Los_Angeles", "America/Los_Angeles", 0},
		{"America/Los_Angeles+12", "America/Los_Angeles", 43200},
		{"America/Los_Angeles-12", "America/Los_Angeles", -43200},
		{"UTC", "UTC", 0},
		{"UTC+1", "UTC", 3600},
		{"UTC+10", "UTC", 36000},
		{"UTC-1", "UTC", -3600},
		{"UTC-10", "UTC", -36000},
	} {
		out, off := parseZone(test.in)
		if g, e := out, test.out; g != e {
			t.Errorf("%d: %+v %v %v", itest, test, g, e)
		}
		if g, e := off, test.off; g != e {
			t.Errorf("%d: %+v %v %v", itest, test, g, e)
		}
	}
}

func TestRint(t *testing.T) {
	tls := NewTLS()
	for itest, test := range []struct {
		x, y float64
	}{
		{-1.1, -1.0},
		{-1.0, -1.0},
		{-0.9, -1.0},
		{-0.51, -1.0},
		{-0.49, 0},
		{-0.1, 0},
		{-0, 0},
		{0.1, 0},
		{0.49, 0},
		{0.51, 1},
		{0.9, 1},
		{1, 1},
		{1.1, 1},
	} {
		if g, e := Xrint(tls, test.x), test.y; g != e {
			t.Errorf("#%d: x %v, got %v, expected %v", itest, test.x, g, e)
		}
	}
}

var testMemsetBuf [67]byte

func TestMemset(t *testing.T) {
	v := 0
	for start := 0; start < len(testMemsetBuf); start++ {
		for n := 0; n < len(testMemsetBuf)-start; n++ {
			for x := range testMemsetBuf {
				testMemsetBuf[x] = byte(v)
				v++
			}
			for x := start; x < start+n; x++ {
				testMemsetBuf[x] = byte(v)
			}
			e := testMemsetBuf
			Xmemset(nil, uintptr(unsafe.Pointer(&testMemsetBuf[start])), int32(v), size_t(n))
			if testMemsetBuf != e {
				t.Fatalf("start %v, v %#x n %v, exp\n%s\ngot\n%s", start, byte(v), n, hex.Dump(e[:]), hex.Dump(testMemsetBuf[:]))
			}
		}
	}
}

const testGetentropySize = 100

var testGetentropyBuf [testGetentropySize]byte

func TestGetentropy(t *testing.T) {
	Xgetentropy(NewTLS(), uintptr(unsafe.Pointer(&testGetentropyBuf[0])), testGetentropySize)
	t.Logf("\n%s", hex.Dump(testGetentropyBuf[:]))
}

func TestReallocArray(t *testing.T) {
	const size = 16
	tls := NewTLS()
	p := Xmalloc(tls, size)
	if p == 0 {
		t.Fatal()
	}

	for i := 0; i < size; i++ {
		(*RawMem)(unsafe.Pointer(p))[i] = byte(i ^ 0x55)
	}

	q := Xreallocarray(tls, p, 2, size)
	if q == 0 {
		t.Fatal()
	}

	for i := 0; i < size; i++ {
		if g, e := (*RawMem)(unsafe.Pointer(q))[i], byte(i^0x55); g != e {
			t.Fatal(i, g, e)
		}
	}
}

func mustCString(s string) uintptr {
	r, err := CString(s)
	if err != nil {
		panic("CString failed")
	}

	return r
}

var testSnprintfBuf [3]byte

func TestSnprintf(t *testing.T) {
	testSnprintfBuf = [3]byte{0xff, 0xff, 0xff}
	p := uintptr(unsafe.Pointer(&testSnprintfBuf[0]))
	if g, e := Xsnprintf(nil, p, 0, 0, 0), int32(0); g != e {
		t.Fatal(g, e)
	}

	if g, e := testSnprintfBuf, [3]byte{0xff, 0xff, 0xff}; g != e {
		t.Fatal(g, e)
	}

	if g, e := Xsnprintf(nil, p, 0, mustCString(""), 0), int32(0); g != e {
		t.Fatal(g, e)
	}

	if g, e := testSnprintfBuf, [3]byte{0xff, 0xff, 0xff}; g != e {
		t.Fatal(g, e)
	}

	s := mustCString("12")
	if g, e := Xsnprintf(nil, p, 0, s, 0), int32(2); g != e {
		t.Fatal(g, e)
	}

	if g, e := testSnprintfBuf, [3]byte{0xff, 0xff, 0xff}; g != e {
		t.Fatal(g, e)
	}

	if g, e := Xsnprintf(nil, p, 1, s, 0), int32(2); g != e {
		t.Fatal(g, e)
	}

	if g, e := testSnprintfBuf, [3]byte{0x00, 0xff, 0xff}; g != e {
		t.Fatal(g, e)
	}

	testSnprintfBuf = [3]byte{0xff, 0xff, 0xff}
	if g, e := Xsnprintf(nil, p, 2, s, 0), int32(2); g != e {
		t.Fatal(g, e)
	}

	if g, e := testSnprintfBuf, [3]byte{'1', 0x00, 0xff}; g != e {
		t.Fatal(g, e)
	}

	testSnprintfBuf = [3]byte{0xff, 0xff, 0xff}
	if g, e := Xsnprintf(nil, p, 3, s, 0), int32(2); g != e {
		t.Fatal(g, e)
	}

	if g, e := testSnprintfBuf, [3]byte{'1', '2', 0x00}; g != e {
		t.Fatal(g, e)
	}
}

var testFdopenBuf [100]byte

func TestFdopen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not implemented on Windows")
	}

	const s = "foobarbaz\n"
	tempdir := t.TempDir()
	f, err := os.Create(filepath.Join(tempdir, "test_fdopen"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}

	if _, err := f.Seek(0, os.SEEK_SET); err != nil {
		t.Fatal(err)
	}

	tls := NewTLS()

	defer tls.Close()

	p := Xfdopen(tls, int32(f.Fd()), mustCString("r"))

	bp := uintptr(unsafe.Pointer(&testFdopenBuf))
	if g, e := Xfread(tls, bp, 1, size_t(len(testFdopenBuf)), p), size_t(len(s)); g != e {
		t.Fatal(g, e)
	}

	if g, e := string(GoBytes(bp, len(s))), s; g != e {
		t.Fatalf("%q %q", g, e)
	}
}

func TestSync(t *testing.T) {
	tls := NewTLS()
	X__sync_synchronize(tls)
	tls.Close()
}

func TestProbes(t *testing.T) {
	p := NewPerfCounter([]string{"a", "b", "c"})
	p.Inc(1)
	t.Logf("====\n%s\n----", p)
	c := NewStackCapture(100)
	for i := 0; i < 20; i++ {
		c.Record()
		if i%3 == 0 {
			c.Record()
		}
	}
	t.Logf("====\n%s\n----", c)
}

var _time ctime.Time_t

func TestGmtime(t *testing.T) {
	tls := NewTLS()
	_time = ctime.Time_t(time.Now().Unix())
	p := Xgmtime(tls, uintptr(unsafe.Pointer(&_time)))
	t.Logf("%v %+v", _time, (*ctime.Tm)(unsafe.Pointer(p)))
	tls.Close()
}
