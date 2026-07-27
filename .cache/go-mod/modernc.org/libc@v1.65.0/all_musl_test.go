// Copyright 2023 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64 || 386 || arm)

package libc // import "modernc.org/libc"

// /tmp/dbg/libc-test/

import (
	"bytes"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	ccgo "modernc.org/ccgo/v4/lib"
	util "modernc.org/fileutil/ccgo"
	"modernc.org/memory"
)

var (
	cpus     = runtime.GOMAXPROCS(-1)
	j        = fmt.Sprint(cpus)
	muslArch string
	target   = fmt.Sprintf("%s/%s", goos, goarch)

	testAtomicCASInt32  int32
	testAtomicCASUint64 uint64
	testAtomicCASp      uintptr

	oRe = flag.String("re", "", "")
	re  *regexp.Regexp
)

func TestMain(m *testing.M) {
	if ccgo.IsExecEnv() {
		if err := ccgo.NewTask(goos, goarch, os.Args, os.Stdout, os.Stderr, nil).Main(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return
	}

	flag.Parse()
	if s := *oRe; s != "" {
		re = regexp.MustCompile(s)
	}

	switch goarch {
	case "386":
		muslArch = "i386"
	case "amd64":
		muslArch = "x86_64"
	case "arm":
		muslArch = "arm"
	case "arm64":
		muslArch = "aarch64"
	case "loong64":
		muslArch = "mips"
	case "ppc64le":
		muslArch = "powerpc64"
	case "riscv64":
		muslArch = "riscv64"
	case "s390x":
		muslArch = "s390x"
	default:
		fmt.Printf("unsupported goarch: %s\n", goarch)
		os.Exit(1)
	}

	rc := m.Run()
	os.Exit(rc)
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

func TestSync(t *testing.T) {
	tls := NewTLS()
	X__sync_synchronize(tls)
	tls.Close()
}

func TestXfmod(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

	x := 1.3518643030646695
	y := 6.283185307179586
	if g, e := Xfmod(tls, x, y), 1.3518643030646695; g != e {
		t.Fatal(g, e)
	}
}

var (
	valist       [256]byte
	formatString [256]byte
	srcString    [256]byte
	printBuf     [256]byte
	testPrintfS1 = [...]byte{'X', 'Y', 0}
)

func TestSprintf(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

	i := uint64(0x123456789abcdef)
	j := uint64(0xf123456789abcde)
	k := uint64(0x23456789abcdef1)
	l := uint64(0xef123456789abcd)
	for itest, test := range []struct {
		fmt    string
		args   []interface{}
		result string
	}{
		// musl 0.5.0 fails
		{
			"%llx %x %llx %x",
			[]interface{}{int64(i), int32(j), int64(k), int32(l)},
			"123456789abcdef 789abcde 23456789abcdef1 6789abcd",
		},
		// musl 0.5.0 panics
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
		printBuf = [256]byte{}
		rc := Xsprintf(tls, uintptr(unsafe.Pointer(&printBuf)), uintptr(unsafe.Pointer(&formatString[0])), VaList(uintptr(unsafe.Pointer(&valist[0])), test.args...))
		x := bytes.IndexByte(printBuf[:], 0)
		if x < 0 {
			t.Errorf("%v:", itest)
			continue
		}

		b := printBuf[:x]
		if g, e := string(b), test.result; g != e {
			t.Errorf("%v: %q %q, rc %v", itest, g, e, rc)
		}
	}
}

func TestStrtod(t *testing.T) {
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

func TestRint(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

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
			Xmemset(nil, uintptr(unsafe.Pointer(&testMemsetBuf[start])), int32(v), Tsize_t(n))
			if testMemsetBuf != e {
				t.Fatalf("start %v, v %#x n %v, exp\n%s\ngot\n%s", start, byte(v), n, hex.Dump(e[:]), hex.Dump(testMemsetBuf[:]))
			}
		}
	}
}

const testGetentropySize = 100

var testGetentropyBuf [testGetentropySize]byte

func TestGetentropy(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

	Xgetentropy(tls, uintptr(unsafe.Pointer(&testGetentropyBuf[0])), testGetentropySize)
	t.Logf("\n%s", hex.Dump(testGetentropyBuf[:]))
}

func TestReallocArray(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

	const size = 16
	p := Xmalloc(tls, size)
	if p == 0 {
		t.Fatal()
	}

	for i := 0; i < size; i++ {
		unsafe.Slice((*byte)(unsafe.Pointer(p)), size)[i] = byte(i ^ 0x55)
	}

	q := Xreallocarray(tls, p, 2, size)
	if q == 0 {
		t.Fatal()
	}

	defer Xfree(tls, q)

	for i := 0; i < size; i++ {
		if g, e := unsafe.Slice((*byte)(unsafe.Pointer(q)), size)[i], byte(i^0x55); g != e {
			t.Fatal(i, g, e)
		}
	}
}

var testSnprintfBuf [3]byte

func TestSnprintf(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

	testSnprintfBuf = [3]byte{0xff, 0xff, 0xff}
	p := uintptr(unsafe.Pointer(&testSnprintfBuf[0]))
	s := mustCString("12")
	if g, e := Xsnprintf(tls, p, 1, s, 0), int32(2); g != e {
		t.Fatal(g, e)
	}

	if g, e := testSnprintfBuf, [3]byte{0x00, 0xff, 0xff}; g != e {
		t.Fatal(g, e)
	}

	testSnprintfBuf = [3]byte{0xff, 0xff, 0xff}
	if g, e := Xsnprintf(tls, p, 2, s, 0), int32(2); g != e {
		t.Fatal(g, e)
	}

	if g, e := testSnprintfBuf, [3]byte{'1', 0x00, 0xff}; g != e {
		t.Fatal(g, e)
	}

	testSnprintfBuf = [3]byte{0xff, 0xff, 0xff}
	if g, e := Xsnprintf(tls, p, 3, s, 0), int32(2); g != e {
		t.Fatal(g, e)
	}

	if g, e := testSnprintfBuf, [3]byte{'1', '2', 0x00}; g != e {
		t.Fatal(g, e)
	}
}

var testFdopenBuf [100]byte

func TestFdopen(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

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

	p := Xfdopen(tls, int32(f.Fd()), mustCString("r"))

	bp := uintptr(unsafe.Pointer(&testFdopenBuf))
	if g, e := Xfread(tls, bp, 1, Tsize_t(len(testFdopenBuf)), p), Tsize_t(len(s)); g != e {
		t.Fatal(g, e)
	}

	if g, e := string(GoBytes(bp, len(s))), s; g != e {
		t.Fatalf("%q %q", g, e)
	}
}

func TestPow(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

	for itest, test := range []struct{ x, y, z float64 }{
		{2, 12, 4096},
	} {
		if g, e := Xpow(tls, test.x, test.y), test.z; g != e {
			t.Errorf("%d: %v %v %v, %v", itest, test.x, test.y, test.z, g)
		}
	}
}

var (
	testGmtimeTm   uintptr
	testGmtimeTime Ttime_t
)

func TestGmtime(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

	testGmtimeTm = Xgmtime(tls, uintptr(unsafe.Pointer(&testGmtimeTime)))
	t.Logf("%+v", (*Ttm)(unsafe.Pointer(testGmtimeTm)))
	if g, e := GoString((*Ttm)(unsafe.Pointer(testGmtimeTm)).F__tm_zone), "UTC"; g != e {
		t.Errorf("0: g=`%v` e=`%s`", g, e)
	}
	(*Ttm)(unsafe.Pointer(testGmtimeTm)).F__tm_zone = 0
	if g, e := *(*Ttm)(unsafe.Pointer(testGmtimeTm)), (Ttm{
		Ftm_mday: 1,
		Ftm_year: 70,
		Ftm_wday: 4,
	}); g != e {
		t.Errorf("0:\ng=%+v\ne=%+v", g, e)
	}
}

var (
	testStrftimeBuf  [1000]byte
	testStrftimeFmt  = mustCString("%d,%e,%F,%H,%k,%I,%l,%j,%m,%M,%u,%w,%W,%Y,%%,%P,%p")
	testStrftimeTm   uintptr
	testStrftimeTime Ttime_t
)

func TestStrftime(t *testing.T) {
	tls := NewTLS()

	defer tls.Close()

	testStrftimeTm = Xgmtime(tls, uintptr(unsafe.Pointer(&testStrftimeTime)))
	t.Logf("%+v", (*Ttm)(unsafe.Pointer(testStrftimeTm)))
	r := Xstrftime(
		tls,
		uintptr(unsafe.Pointer(&testStrftimeBuf[0])), Tsize_t(len(testStrftimeBuf)),
		testStrftimeFmt, testStrftimeTm,
	)
	if g, e := GoString(uintptr(unsafe.Pointer(&testStrftimeBuf[0]))), "01, 1,1970-01-01,00, 0,12,12,001,01,00,4,4,00,1970,%,am,AM"; g != e {
		t.Errorf("0: r=%v g=`%s` e=`%s`", r, g, e)
	}
	_ = r
}

func TestMemAuditBrk(t *testing.T) {
	if !isMemBrk {
		t.Skip("requires -tags=libc.membrk")
	}

	var sv memory.Allocator
	sv, allocator = allocator, sv

	defer func() { allocator = sv }()

	mallocP := mustMalloc(1)
	t.Logf("mallocP %v %#0[1]x", mallocP)
	t.Logf("\n%s", hex.Dump(unsafe.Slice((*byte)(unsafe.Pointer(mallocP-heapGuard)), 4*heapGuard)))
	q := mallocP - heapGuard
	c := 0
	for ; q < mallocP; q++ {
		*(*byte)(unsafe.Pointer(q)) ^= 0x55
		c++
	}

	z := roundup(mallocP+1, heapAlign)
	for p := mallocP + 1; p < z; p++ {
		*(*byte)(unsafe.Pointer(p)) ^= 0x55
	}
	p := z
	z += heapGuard
	for ; p < z; p++ {
		*(*byte)(unsafe.Pointer(p)) ^= 0x55
		c++
	}
	p = mallocP + 2*heapGuard + 7
	*(*byte)(unsafe.Pointer(p)) ^= 0x55
	c++
	t.Logf("c %v, \n%s", c, hex.Dump(unsafe.Slice((*byte)(unsafe.Pointer(mallocP-heapGuard)), 4*heapGuard)))
	r := MemAudit()
	for i, v := range r {
		t.Log(i, v)
	}
	if g, e := len(r), c; g != e {
		t.Fatalf("got %v errors, expected %v", g, e)
	}
}

func mustShell(t *testing.T, max time.Duration, bin string, args ...string) (out []byte) {
	var err error
	out, err = shell(max, bin, args...)
	if err != nil {
		t.Fatalf("FAIL err=%v out=%s", err, out)
	}

	return out
}

func mustCopyDir(t *testing.T, dst, src string, canOverwrite func(fn string, fi os.FileInfo) bool) (files int, bytes int64) {
	files, bytes, err := util.CopyDir(dst, src, canOverwrite)
	if err != nil {
		t.Fatal(err)
	}

	return files, bytes
}

func mustInDir(t *testing.T, dir string, f func() error) {
	if err := util.InDir(dir, f); err != nil {
		t.Fatalf("FAIL err=%v", err)
	}
}

func shell(max time.Duration, bin string, args ...string) (out []byte, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), max)

	defer cancel()

	return util.Shell(ctx, bin, args...)
}

type parallel struct {
	blacklist map[string]struct{}
	errs      []error
	limit     chan struct{}
	passed    []string
	sync.Mutex
	t  *testing.T
	wd string
	wg sync.WaitGroup

	buildFails atomic.Int32
	execFails  atomic.Int32
	files      atomic.Int32
	id         atomic.Int32
	pass       atomic.Int32
	skip       atomic.Int32
}

func newParallel(t *testing.T, cpus int, blacklist map[string]struct{}) *parallel {
	return &parallel{
		blacklist: blacklist,
		limit:     make(chan struct{}, cpus),
		t:         t,
		wd:        util.MustAbsCwd(true),
	}
}

func (p *parallel) start(path string) {
	p.wg.Add(1)
	p.limit <- struct{}{}
	p.files.Add(1)

	go p.run(path)
}

func (p *parallel) addError(err error) {
	p.Lock()
	p.errs = append(p.errs, err)
	p.Unlock()
}

func (p *parallel) run(path string) {
	defer func() {
		<-p.limit
		p.wg.Done()
	}()

	bin := path + ".bin"
	if _, err := shell(10*time.Minute, "go", "build", "-o", bin, path); err != nil {
		// p.t.Logf("%v: BUILD FAIL err=%v out=%s", path, err, out)
		switch _, ok := p.blacklist[path]; {
		case ok:
			p.skip.Add(1)
		default:
			p.buildFails.Add(1)
			p.addError(fmt.Errorf("%v: BUILD FAIL err=%v", path, err))
		}
		return
	}

	if out, err := shell(10*time.Minute, bin); err != nil {
		switch s := fmt.Sprintf("%s %s", out, err); {
		case
			strings.Contains(s, "Function not implemented"),
			strings.Contains(s, "assembler statements not supported"),
			strings.Contains(s, "dlopen failed"):

			p.skip.Add(1)
		default:
			// p.t.Logf("%v: EXEC FAIL err=%v out=%s", path, err, out)
			switch _, ok := p.blacklist[path]; {
			case ok:
				p.skip.Add(1)
			default:
				p.execFails.Add(1)
				p.addError(fmt.Errorf("%v: EXEC FAIL err=%v", path, err))
			}
		}
		return
	}

	p.Lock()
	p.passed = append(p.passed, path)
	p.Unlock()
	p.pass.Add(1)
}

var blacklists = map[string]map[string]struct{}{
	"linux/arm": {
		"src/api/main.exe.go":                                  {},
		"src/functional/pthread_cancel-points-static.exe.go":   {},
		"src/functional/pthread_cancel-points.exe.go":          {},
		"src/functional/pthread_cancel-static.exe.go":          {},
		"src/functional/pthread_cancel.exe.go":                 {},
		"src/functional/pthread_mutex-static.exe.go":           {},
		"src/functional/pthread_mutex.exe.go":                  {},
		"src/functional/pthread_mutex_pi-static.exe.go":        {},
		"src/functional/pthread_mutex_pi.exe.go":               {},
		"src/functional/pthread_robust-static.exe.go":          {},
		"src/functional/pthread_robust.exe.go":                 {},
		"src/functional/sem_init-static.exe.go":                {},
		"src/functional/sem_init.exe.go":                       {},
		"src/functional/sem_open-static.exe.go":                {},
		"src/functional/sem_open.exe.go":                       {},
		"src/functional/setjmp-static.exe.go":                  {},
		"src/functional/setjmp.exe.go":                         {},
		"src/functional/spawn-static.exe.go":                   {},
		"src/functional/spawn.exe.go":                          {},
		"src/math/acos.exe.go":                                 {},
		"src/math/acosf.exe.go":                                {},
		"src/math/acosh.exe.go":                                {},
		"src/math/acoshf.exe.go":                               {},
		"src/math/acoshl.exe.go":                               {},
		"src/math/acosl.exe.go":                                {},
		"src/math/asin.exe.go":                                 {},
		"src/math/asinf.exe.go":                                {},
		"src/math/asinh.exe.go":                                {},
		"src/math/asinhf.exe.go":                               {},
		"src/math/asinhl.exe.go":                               {},
		"src/math/asinl.exe.go":                                {},
		"src/math/atan.exe.go":                                 {},
		"src/math/atan2.exe.go":                                {},
		"src/math/atan2f.exe.go":                               {},
		"src/math/atan2l.exe.go":                               {},
		"src/math/atanf.exe.go":                                {},
		"src/math/atanh.exe.go":                                {},
		"src/math/atanhf.exe.go":                               {},
		"src/math/atanhl.exe.go":                               {},
		"src/math/atanl.exe.go":                                {},
		"src/math/cbrt.exe.go":                                 {},
		"src/math/cbrtf.exe.go":                                {},
		"src/math/cbrtl.exe.go":                                {},
		"src/math/ceil.exe.go":                                 {},
		"src/math/ceilf.exe.go":                                {},
		"src/math/ceill.exe.go":                                {},
		"src/math/copysign.exe.go":                             {},
		"src/math/copysignf.exe.go":                            {},
		"src/math/copysignl.exe.go":                            {},
		"src/math/cos.exe.go":                                  {},
		"src/math/cosf.exe.go":                                 {},
		"src/math/cosh.exe.go":                                 {},
		"src/math/coshf.exe.go":                                {},
		"src/math/coshl.exe.go":                                {},
		"src/math/cosl.exe.go":                                 {},
		"src/math/drem.exe.go":                                 {},
		"src/math/dremf.exe.go":                                {},
		"src/math/erf.exe.go":                                  {},
		"src/math/erfc.exe.go":                                 {},
		"src/math/erfcf.exe.go":                                {},
		"src/math/erfcl.exe.go":                                {},
		"src/math/erff.exe.go":                                 {},
		"src/math/erfl.exe.go":                                 {},
		"src/math/exp.exe.go":                                  {},
		"src/math/exp10.exe.go":                                {},
		"src/math/exp10f.exe.go":                               {},
		"src/math/exp10l.exe.go":                               {},
		"src/math/exp2.exe.go":                                 {},
		"src/math/exp2f.exe.go":                                {},
		"src/math/exp2l.exe.go":                                {},
		"src/math/expf.exe.go":                                 {},
		"src/math/expl.exe.go":                                 {},
		"src/math/expm1.exe.go":                                {},
		"src/math/expm1f.exe.go":                               {},
		"src/math/expm1l.exe.go":                               {},
		"src/math/fabs.exe.go":                                 {},
		"src/math/fabsf.exe.go":                                {},
		"src/math/fabsl.exe.go":                                {},
		"src/math/fdim.exe.go":                                 {},
		"src/math/fdimf.exe.go":                                {},
		"src/math/fdiml.exe.go":                                {},
		"src/math/fenv.exe.go":                                 {},
		"src/math/floor.exe.go":                                {},
		"src/math/floorf.exe.go":                               {},
		"src/math/floorl.exe.go":                               {},
		"src/math/fma.exe.go":                                  {},
		"src/math/fmaf.exe.go":                                 {},
		"src/math/fmal.exe.go":                                 {},
		"src/math/fmax.exe.go":                                 {},
		"src/math/fmaxf.exe.go":                                {},
		"src/math/fmaxl.exe.go":                                {},
		"src/math/fmin.exe.go":                                 {},
		"src/math/fminf.exe.go":                                {},
		"src/math/fminl.exe.go":                                {},
		"src/math/fmod.exe.go":                                 {},
		"src/math/fmodf.exe.go":                                {},
		"src/math/fmodl.exe.go":                                {},
		"src/math/frexp.exe.go":                                {},
		"src/math/frexpf.exe.go":                               {},
		"src/math/frexpl.exe.go":                               {},
		"src/math/hypot.exe.go":                                {},
		"src/math/hypotf.exe.go":                               {},
		"src/math/hypotl.exe.go":                               {},
		"src/math/ilogb.exe.go":                                {},
		"src/math/ilogbf.exe.go":                               {},
		"src/math/ilogbl.exe.go":                               {},
		"src/math/isless.exe.go":                               {},
		"src/math/j0.exe.go":                                   {},
		"src/math/j0f.exe.go":                                  {},
		"src/math/j1.exe.go":                                   {},
		"src/math/j1f.exe.go":                                  {},
		"src/math/jn.exe.go":                                   {},
		"src/math/jnf.exe.go":                                  {},
		"src/math/ldexp.exe.go":                                {},
		"src/math/ldexpf.exe.go":                               {},
		"src/math/ldexpl.exe.go":                               {},
		"src/math/lgamma.exe.go":                               {},
		"src/math/lgamma_r.exe.go":                             {},
		"src/math/lgammaf.exe.go":                              {},
		"src/math/lgammaf_r.exe.go":                            {},
		"src/math/lgammal.exe.go":                              {},
		"src/math/lgammal_r.exe.go":                            {},
		"src/math/llrint.exe.go":                               {},
		"src/math/llrintf.exe.go":                              {},
		"src/math/llrintl.exe.go":                              {},
		"src/math/llround.exe.go":                              {},
		"src/math/llroundf.exe.go":                             {},
		"src/math/llroundl.exe.go":                             {},
		"src/math/log.exe.go":                                  {},
		"src/math/log10.exe.go":                                {},
		"src/math/log10f.exe.go":                               {},
		"src/math/log10l.exe.go":                               {},
		"src/math/log1p.exe.go":                                {},
		"src/math/log1pf.exe.go":                               {},
		"src/math/log1pl.exe.go":                               {},
		"src/math/log2.exe.go":                                 {},
		"src/math/log2f.exe.go":                                {},
		"src/math/log2l.exe.go":                                {},
		"src/math/logb.exe.go":                                 {},
		"src/math/logbf.exe.go":                                {},
		"src/math/logbl.exe.go":                                {},
		"src/math/logf.exe.go":                                 {},
		"src/math/logl.exe.go":                                 {},
		"src/math/lrint.exe.go":                                {},
		"src/math/lrintf.exe.go":                               {},
		"src/math/lrintl.exe.go":                               {},
		"src/math/lround.exe.go":                               {},
		"src/math/lroundf.exe.go":                              {},
		"src/math/lroundl.exe.go":                              {},
		"src/math/modf.exe.go":                                 {},
		"src/math/modff.exe.go":                                {},
		"src/math/modfl.exe.go":                                {},
		"src/math/nearbyint.exe.go":                            {},
		"src/math/nearbyintf.exe.go":                           {},
		"src/math/nearbyintl.exe.go":                           {},
		"src/math/nextafter.exe.go":                            {},
		"src/math/nextafterf.exe.go":                           {},
		"src/math/nextafterl.exe.go":                           {},
		"src/math/nexttoward.exe.go":                           {},
		"src/math/nexttowardf.exe.go":                          {},
		"src/math/nexttowardl.exe.go":                          {},
		"src/math/pow.exe.go":                                  {},
		"src/math/pow10.exe.go":                                {},
		"src/math/pow10f.exe.go":                               {},
		"src/math/pow10l.exe.go":                               {},
		"src/math/powf.exe.go":                                 {},
		"src/math/powl.exe.go":                                 {},
		"src/math/remainder.exe.go":                            {},
		"src/math/remainderf.exe.go":                           {},
		"src/math/remainderl.exe.go":                           {},
		"src/math/remquo.exe.go":                               {},
		"src/math/remquof.exe.go":                              {},
		"src/math/remquol.exe.go":                              {},
		"src/math/rint.exe.go":                                 {},
		"src/math/rintf.exe.go":                                {},
		"src/math/rintl.exe.go":                                {},
		"src/math/round.exe.go":                                {},
		"src/math/roundf.exe.go":                               {},
		"src/math/roundl.exe.go":                               {},
		"src/math/scalb.exe.go":                                {},
		"src/math/scalbf.exe.go":                               {},
		"src/math/scalbln.exe.go":                              {},
		"src/math/scalblnf.exe.go":                             {},
		"src/math/scalblnl.exe.go":                             {},
		"src/math/scalbn.exe.go":                               {},
		"src/math/scalbnf.exe.go":                              {},
		"src/math/scalbnl.exe.go":                              {},
		"src/math/sin.exe.go":                                  {},
		"src/math/sincos.exe.go":                               {},
		"src/math/sincosf.exe.go":                              {},
		"src/math/sincosl.exe.go":                              {},
		"src/math/sinf.exe.go":                                 {},
		"src/math/sinh.exe.go":                                 {},
		"src/math/sinhf.exe.go":                                {},
		"src/math/sinhl.exe.go":                                {},
		"src/math/sinl.exe.go":                                 {},
		"src/math/sqrt.exe.go":                                 {},
		"src/math/sqrtf.exe.go":                                {},
		"src/math/sqrtl.exe.go":                                {},
		"src/math/tan.exe.go":                                  {},
		"src/math/tanf.exe.go":                                 {},
		"src/math/tanh.exe.go":                                 {},
		"src/math/tanhf.exe.go":                                {},
		"src/math/tanhl.exe.go":                                {},
		"src/math/tanl.exe.go":                                 {},
		"src/math/tgamma.exe.go":                               {},
		"src/math/tgammaf.exe.go":                              {},
		"src/math/tgammal.exe.go":                              {},
		"src/math/trunc.exe.go":                                {},
		"src/math/truncf.exe.go":                               {},
		"src/math/truncl.exe.go":                               {},
		"src/math/y0.exe.go":                                   {},
		"src/math/y0f.exe.go":                                  {},
		"src/math/y1.exe.go":                                   {},
		"src/math/y1f.exe.go":                                  {},
		"src/math/yn.exe.go":                                   {},
		"src/math/ynf.exe.go":                                  {},
		"src/regression/daemon-failure-static.exe.go":          {},
		"src/regression/daemon-failure.exe.go":                 {},
		"src/regression/pthread-robust-detach-static.exe.go":   {},
		"src/regression/pthread-robust-detach.exe.go":          {},
		"src/regression/pthread_cancel-sem_wait-static.exe.go": {},
		"src/regression/pthread_cancel-sem_wait.exe.go":        {},
		"src/regression/pthread_cond_wait-cancel_ignored-static.exe.go": {},
		"src/regression/pthread_cond_wait-cancel_ignored.exe.go":        {},
		"src/regression/pthread_condattr_setclock-static.exe.go":        {},
		"src/regression/pthread_condattr_setclock.exe.go":               {},
		"src/regression/pthread_once-deadlock-static.exe.go":            {},
		"src/regression/pthread_once-deadlock.exe.go":                   {},
		"src/regression/pthread_rwlock-ebusy-static.exe.go":             {},
		"src/regression/pthread_rwlock-ebusy.exe.go":                    {},
		"src/regression/raise-race-static.exe.go":                       {},
		"src/regression/raise-race.exe.go":                              {},
		"src/regression/sem_close-unmap-static.exe.go":                  {},
		"src/regression/sem_close-unmap.exe.go":                         {},
		"src/regression/tls_get_new-dtv.exe.go":                         {},

		//TODO EXEC FAIL
		"src/common/runtest.exe.go":                       {},
		"src/functional/dlopen.exe.go":                    {},
		"src/functional/popen-static.exe.go":              {},
		"src/functional/popen.exe.go":                     {},
		"src/functional/sscanf-static.exe.go":             {},
		"src/functional/sscanf.exe.go":                    {},
		"src/functional/strptime-static.exe.go":           {},
		"src/functional/strptime.exe.go":                  {},
		"src/functional/tgmath-static.exe.go":             {},
		"src/functional/tgmath.exe.go":                    {},
		"src/functional/tls_align-static.exe.go":          {},
		"src/functional/tls_init-static.exe.go":           {},
		"src/functional/tls_init.exe.go":                  {},
		"src/functional/tls_local_exec-static.exe.go":     {},
		"src/functional/tls_local_exec.exe.go":            {},
		"src/regression/flockfile-list-static.exe.go":     {},
		"src/regression/flockfile-list.exe.go":            {},
		"src/regression/malloc-brk-fail-static.exe.go":    {},
		"src/regression/malloc-brk-fail.exe.go":           {},
		"src/regression/malloc-oom-static.exe.go":         {},
		"src/regression/malloc-oom.exe.go":                {},
		"src/regression/pthread_create-oom-static.exe.go": {},
		"src/regression/pthread_create-oom.exe.go":        {},
		"src/regression/setenv-oom-static.exe.go":         {},
		"src/regression/setenv-oom.exe.go":                {},
		"src/regression/sigaltstack-static.exe.go":        {},
		"src/regression/sigaltstack.exe.go":               {},
		"src/regression/sigreturn-static.exe.go":          {},
		"src/regression/sigreturn.exe.go":                 {},
	},
	"linux/386": {
		"src/api/main.exe.go":                                  {},
		"src/functional/pthread_cancel-points-static.exe.go":   {},
		"src/functional/pthread_cancel-points.exe.go":          {},
		"src/functional/pthread_cancel-static.exe.go":          {},
		"src/functional/pthread_cancel.exe.go":                 {},
		"src/functional/pthread_mutex-static.exe.go":           {},
		"src/functional/pthread_mutex.exe.go":                  {},
		"src/functional/pthread_mutex_pi-static.exe.go":        {},
		"src/functional/pthread_mutex_pi.exe.go":               {},
		"src/functional/pthread_robust-static.exe.go":          {},
		"src/functional/pthread_robust.exe.go":                 {},
		"src/functional/sem_init-static.exe.go":                {},
		"src/functional/sem_init.exe.go":                       {},
		"src/functional/sem_open-static.exe.go":                {},
		"src/functional/sem_open.exe.go":                       {},
		"src/functional/setjmp-static.exe.go":                  {},
		"src/functional/setjmp.exe.go":                         {},
		"src/functional/spawn-static.exe.go":                   {},
		"src/functional/spawn.exe.go":                          {},
		"src/math/acos.exe.go":                                 {},
		"src/math/acosf.exe.go":                                {},
		"src/math/acosh.exe.go":                                {},
		"src/math/acoshf.exe.go":                               {},
		"src/math/acoshl.exe.go":                               {},
		"src/math/acosl.exe.go":                                {},
		"src/math/asin.exe.go":                                 {},
		"src/math/asinf.exe.go":                                {},
		"src/math/asinh.exe.go":                                {},
		"src/math/asinhf.exe.go":                               {},
		"src/math/asinhl.exe.go":                               {},
		"src/math/asinl.exe.go":                                {},
		"src/math/atan.exe.go":                                 {},
		"src/math/atan2.exe.go":                                {},
		"src/math/atan2f.exe.go":                               {},
		"src/math/atan2l.exe.go":                               {},
		"src/math/atanf.exe.go":                                {},
		"src/math/atanh.exe.go":                                {},
		"src/math/atanhf.exe.go":                               {},
		"src/math/atanhl.exe.go":                               {},
		"src/math/atanl.exe.go":                                {},
		"src/math/cbrt.exe.go":                                 {},
		"src/math/cbrtf.exe.go":                                {},
		"src/math/cbrtl.exe.go":                                {},
		"src/math/ceil.exe.go":                                 {},
		"src/math/ceilf.exe.go":                                {},
		"src/math/ceill.exe.go":                                {},
		"src/math/copysign.exe.go":                             {},
		"src/math/copysignf.exe.go":                            {},
		"src/math/copysignl.exe.go":                            {},
		"src/math/cos.exe.go":                                  {},
		"src/math/cosf.exe.go":                                 {},
		"src/math/cosh.exe.go":                                 {},
		"src/math/coshf.exe.go":                                {},
		"src/math/coshl.exe.go":                                {},
		"src/math/cosl.exe.go":                                 {},
		"src/math/drem.exe.go":                                 {},
		"src/math/dremf.exe.go":                                {},
		"src/math/erf.exe.go":                                  {},
		"src/math/erfc.exe.go":                                 {},
		"src/math/erfcf.exe.go":                                {},
		"src/math/erfcl.exe.go":                                {},
		"src/math/erff.exe.go":                                 {},
		"src/math/erfl.exe.go":                                 {},
		"src/math/exp.exe.go":                                  {},
		"src/math/exp10.exe.go":                                {},
		"src/math/exp10f.exe.go":                               {},
		"src/math/exp10l.exe.go":                               {},
		"src/math/exp2.exe.go":                                 {},
		"src/math/exp2f.exe.go":                                {},
		"src/math/exp2l.exe.go":                                {},
		"src/math/expf.exe.go":                                 {},
		"src/math/expl.exe.go":                                 {},
		"src/math/expm1.exe.go":                                {},
		"src/math/expm1f.exe.go":                               {},
		"src/math/expm1l.exe.go":                               {},
		"src/math/fabs.exe.go":                                 {},
		"src/math/fabsf.exe.go":                                {},
		"src/math/fabsl.exe.go":                                {},
		"src/math/fdim.exe.go":                                 {},
		"src/math/fdimf.exe.go":                                {},
		"src/math/fdiml.exe.go":                                {},
		"src/math/fenv.exe.go":                                 {},
		"src/math/floor.exe.go":                                {},
		"src/math/floorf.exe.go":                               {},
		"src/math/floorl.exe.go":                               {},
		"src/math/fma.exe.go":                                  {},
		"src/math/fmaf.exe.go":                                 {},
		"src/math/fmal.exe.go":                                 {},
		"src/math/fmax.exe.go":                                 {},
		"src/math/fmaxf.exe.go":                                {},
		"src/math/fmaxl.exe.go":                                {},
		"src/math/fmin.exe.go":                                 {},
		"src/math/fminf.exe.go":                                {},
		"src/math/fminl.exe.go":                                {},
		"src/math/fmod.exe.go":                                 {},
		"src/math/fmodf.exe.go":                                {},
		"src/math/fmodl.exe.go":                                {},
		"src/math/frexp.exe.go":                                {},
		"src/math/frexpf.exe.go":                               {},
		"src/math/frexpl.exe.go":                               {},
		"src/math/hypot.exe.go":                                {},
		"src/math/hypotf.exe.go":                               {},
		"src/math/hypotl.exe.go":                               {},
		"src/math/ilogb.exe.go":                                {},
		"src/math/ilogbf.exe.go":                               {},
		"src/math/ilogbl.exe.go":                               {},
		"src/math/j0.exe.go":                                   {},
		"src/math/j0f.exe.go":                                  {},
		"src/math/j1.exe.go":                                   {},
		"src/math/j1f.exe.go":                                  {},
		"src/math/jn.exe.go":                                   {},
		"src/math/jnf.exe.go":                                  {},
		"src/math/ldexp.exe.go":                                {},
		"src/math/ldexpf.exe.go":                               {},
		"src/math/ldexpl.exe.go":                               {},
		"src/math/lgamma.exe.go":                               {},
		"src/math/lgamma_r.exe.go":                             {},
		"src/math/lgammaf.exe.go":                              {},
		"src/math/lgammaf_r.exe.go":                            {},
		"src/math/lgammal.exe.go":                              {},
		"src/math/lgammal_r.exe.go":                            {},
		"src/math/llrint.exe.go":                               {},
		"src/math/llrintf.exe.go":                              {},
		"src/math/llrintl.exe.go":                              {},
		"src/math/llround.exe.go":                              {},
		"src/math/llroundf.exe.go":                             {},
		"src/math/llroundl.exe.go":                             {},
		"src/math/log.exe.go":                                  {},
		"src/math/log10.exe.go":                                {},
		"src/math/log10f.exe.go":                               {},
		"src/math/log10l.exe.go":                               {},
		"src/math/log1p.exe.go":                                {},
		"src/math/log1pf.exe.go":                               {},
		"src/math/log1pl.exe.go":                               {},
		"src/math/log2.exe.go":                                 {},
		"src/math/log2f.exe.go":                                {},
		"src/math/log2l.exe.go":                                {},
		"src/math/logb.exe.go":                                 {},
		"src/math/logbf.exe.go":                                {},
		"src/math/logbl.exe.go":                                {},
		"src/math/logf.exe.go":                                 {},
		"src/math/logl.exe.go":                                 {},
		"src/math/lrint.exe.go":                                {},
		"src/math/lrintf.exe.go":                               {},
		"src/math/lrintl.exe.go":                               {},
		"src/math/lround.exe.go":                               {},
		"src/math/lroundf.exe.go":                              {},
		"src/math/lroundl.exe.go":                              {},
		"src/math/modf.exe.go":                                 {},
		"src/math/modff.exe.go":                                {},
		"src/math/modfl.exe.go":                                {},
		"src/math/nearbyint.exe.go":                            {},
		"src/math/nearbyintf.exe.go":                           {},
		"src/math/nearbyintl.exe.go":                           {},
		"src/math/nextafter.exe.go":                            {},
		"src/math/nextafterf.exe.go":                           {},
		"src/math/nextafterl.exe.go":                           {},
		"src/math/nexttoward.exe.go":                           {},
		"src/math/nexttowardf.exe.go":                          {},
		"src/math/nexttowardl.exe.go":                          {},
		"src/math/pow.exe.go":                                  {},
		"src/math/pow10.exe.go":                                {},
		"src/math/pow10f.exe.go":                               {},
		"src/math/pow10l.exe.go":                               {},
		"src/math/powf.exe.go":                                 {},
		"src/math/powl.exe.go":                                 {},
		"src/math/remainder.exe.go":                            {},
		"src/math/remainderf.exe.go":                           {},
		"src/math/remainderl.exe.go":                           {},
		"src/math/remquo.exe.go":                               {},
		"src/math/remquof.exe.go":                              {},
		"src/math/remquol.exe.go":                              {},
		"src/math/rint.exe.go":                                 {},
		"src/math/rintf.exe.go":                                {},
		"src/math/rintl.exe.go":                                {},
		"src/math/round.exe.go":                                {},
		"src/math/roundf.exe.go":                               {},
		"src/math/roundl.exe.go":                               {},
		"src/math/scalb.exe.go":                                {},
		"src/math/scalbf.exe.go":                               {},
		"src/math/scalbln.exe.go":                              {},
		"src/math/scalblnf.exe.go":                             {},
		"src/math/scalblnl.exe.go":                             {},
		"src/math/scalbn.exe.go":                               {},
		"src/math/scalbnf.exe.go":                              {},
		"src/math/scalbnl.exe.go":                              {},
		"src/math/sin.exe.go":                                  {},
		"src/math/sincos.exe.go":                               {},
		"src/math/sincosf.exe.go":                              {},
		"src/math/sincosl.exe.go":                              {},
		"src/math/sinf.exe.go":                                 {},
		"src/math/sinh.exe.go":                                 {},
		"src/math/sinhf.exe.go":                                {},
		"src/math/sinhl.exe.go":                                {},
		"src/math/sinl.exe.go":                                 {},
		"src/math/sqrt.exe.go":                                 {},
		"src/math/sqrtf.exe.go":                                {},
		"src/math/sqrtl.exe.go":                                {},
		"src/math/tan.exe.go":                                  {},
		"src/math/tanf.exe.go":                                 {},
		"src/math/tanh.exe.go":                                 {},
		"src/math/tanhf.exe.go":                                {},
		"src/math/tanhl.exe.go":                                {},
		"src/math/tanl.exe.go":                                 {},
		"src/math/tgamma.exe.go":                               {},
		"src/math/tgammaf.exe.go":                              {},
		"src/math/tgammal.exe.go":                              {},
		"src/math/trunc.exe.go":                                {},
		"src/math/truncf.exe.go":                               {},
		"src/math/truncl.exe.go":                               {},
		"src/math/y0.exe.go":                                   {},
		"src/math/y0f.exe.go":                                  {},
		"src/math/y1.exe.go":                                   {},
		"src/math/y1f.exe.go":                                  {},
		"src/math/yn.exe.go":                                   {},
		"src/math/ynf.exe.go":                                  {},
		"src/regression/daemon-failure-static.exe.go":          {},
		"src/regression/daemon-failure.exe.go":                 {},
		"src/regression/pthread-robust-detach-static.exe.go":   {},
		"src/regression/pthread-robust-detach.exe.go":          {},
		"src/regression/pthread_cancel-sem_wait-static.exe.go": {},
		"src/regression/pthread_cancel-sem_wait.exe.go":        {},
		"src/regression/pthread_cond_wait-cancel_ignored-static.exe.go": {},
		"src/regression/pthread_cond_wait-cancel_ignored.exe.go":        {},
		"src/regression/pthread_condattr_setclock-static.exe.go":        {},
		"src/regression/pthread_condattr_setclock.exe.go":               {},
		"src/regression/pthread_once-deadlock-static.exe.go":            {},
		"src/regression/pthread_once-deadlock.exe.go":                   {},
		"src/regression/pthread_rwlock-ebusy-static.exe.go":             {},
		"src/regression/pthread_rwlock-ebusy.exe.go":                    {},
		"src/regression/raise-race-static.exe.go":                       {},
		"src/regression/raise-race.exe.go":                              {},
		"src/regression/sem_close-unmap-static.exe.go":                  {},
		"src/regression/sem_close-unmap.exe.go":                         {},
		"src/regression/tls_get_new-dtv.exe.go":                         {},

		//TODO EXEC FAIL
		"src/common/runtest.exe.go":                       {},
		"src/functional/dlopen.exe.go":                    {},
		"src/functional/popen-static.exe.go":              {},
		"src/functional/popen.exe.go":                     {},
		"src/functional/sscanf-static.exe.go":             {},
		"src/functional/sscanf.exe.go":                    {},
		"src/functional/strptime-static.exe.go":           {},
		"src/functional/strptime.exe.go":                  {},
		"src/functional/tgmath-static.exe.go":             {},
		"src/functional/tgmath.exe.go":                    {},
		"src/functional/tls_align-static.exe.go":          {},
		"src/functional/tls_init-static.exe.go":           {},
		"src/functional/tls_init.exe.go":                  {},
		"src/functional/tls_local_exec-static.exe.go":     {},
		"src/functional/tls_local_exec.exe.go":            {},
		"src/regression/flockfile-list-static.exe.go":     {},
		"src/regression/flockfile-list.exe.go":            {},
		"src/regression/malloc-brk-fail-static.exe.go":    {},
		"src/regression/malloc-brk-fail.exe.go":           {},
		"src/regression/pthread_create-oom-static.exe.go": {},
		"src/regression/pthread_create-oom.exe.go":        {},
		"src/regression/setenv-oom-static.exe.go":         {},
		"src/regression/setenv-oom.exe.go":                {},
		"src/regression/sigaltstack-static.exe.go":        {},
		"src/regression/sigaltstack.exe.go":               {},
		"src/regression/sigreturn-static.exe.go":          {},
		"src/regression/sigreturn.exe.go":                 {},
	},
	"linux/riscv64": {
		"src/api/main.exe.go":                                  {},
		"src/functional/pthread_cancel-points-static.exe.go":   {},
		"src/functional/pthread_cancel-points.exe.go":          {},
		"src/functional/pthread_cancel-static.exe.go":          {},
		"src/functional/pthread_cancel.exe.go":                 {},
		"src/functional/pthread_mutex-static.exe.go":           {},
		"src/functional/pthread_mutex.exe.go":                  {},
		"src/functional/pthread_mutex_pi-static.exe.go":        {},
		"src/functional/pthread_mutex_pi.exe.go":               {},
		"src/functional/pthread_robust-static.exe.go":          {},
		"src/functional/pthread_robust.exe.go":                 {},
		"src/functional/sem_init-static.exe.go":                {},
		"src/functional/sem_init.exe.go":                       {},
		"src/functional/sem_open-static.exe.go":                {},
		"src/functional/sem_open.exe.go":                       {},
		"src/functional/setjmp-static.exe.go":                  {},
		"src/functional/setjmp.exe.go":                         {},
		"src/functional/spawn-static.exe.go":                   {},
		"src/functional/spawn.exe.go":                          {},
		"src/math/acos.exe.go":                                 {},
		"src/math/acosf.exe.go":                                {},
		"src/math/acosh.exe.go":                                {},
		"src/math/acoshf.exe.go":                               {},
		"src/math/acoshl.exe.go":                               {},
		"src/math/acosl.exe.go":                                {},
		"src/math/asin.exe.go":                                 {},
		"src/math/asinf.exe.go":                                {},
		"src/math/asinh.exe.go":                                {},
		"src/math/asinhf.exe.go":                               {},
		"src/math/asinhl.exe.go":                               {},
		"src/math/asinl.exe.go":                                {},
		"src/math/atan.exe.go":                                 {},
		"src/math/atan2.exe.go":                                {},
		"src/math/atan2f.exe.go":                               {},
		"src/math/atan2l.exe.go":                               {},
		"src/math/atanf.exe.go":                                {},
		"src/math/atanh.exe.go":                                {},
		"src/math/atanhf.exe.go":                               {},
		"src/math/atanhl.exe.go":                               {},
		"src/math/atanl.exe.go":                                {},
		"src/math/cbrt.exe.go":                                 {},
		"src/math/cbrtf.exe.go":                                {},
		"src/math/cbrtl.exe.go":                                {},
		"src/math/ceil.exe.go":                                 {},
		"src/math/ceilf.exe.go":                                {},
		"src/math/ceill.exe.go":                                {},
		"src/math/copysign.exe.go":                             {},
		"src/math/copysignf.exe.go":                            {},
		"src/math/copysignl.exe.go":                            {},
		"src/math/cos.exe.go":                                  {},
		"src/math/cosf.exe.go":                                 {},
		"src/math/cosh.exe.go":                                 {},
		"src/math/coshf.exe.go":                                {},
		"src/math/coshl.exe.go":                                {},
		"src/math/cosl.exe.go":                                 {},
		"src/math/drem.exe.go":                                 {},
		"src/math/dremf.exe.go":                                {},
		"src/math/erf.exe.go":                                  {},
		"src/math/erfc.exe.go":                                 {},
		"src/math/erfcf.exe.go":                                {},
		"src/math/erfcl.exe.go":                                {},
		"src/math/erff.exe.go":                                 {},
		"src/math/erfl.exe.go":                                 {},
		"src/math/exp.exe.go":                                  {},
		"src/math/exp10.exe.go":                                {},
		"src/math/exp10f.exe.go":                               {},
		"src/math/exp10l.exe.go":                               {},
		"src/math/exp2.exe.go":                                 {},
		"src/math/exp2f.exe.go":                                {},
		"src/math/exp2l.exe.go":                                {},
		"src/math/expf.exe.go":                                 {},
		"src/math/expl.exe.go":                                 {},
		"src/math/expm1.exe.go":                                {},
		"src/math/expm1f.exe.go":                               {},
		"src/math/expm1l.exe.go":                               {},
		"src/math/fabs.exe.go":                                 {},
		"src/math/fabsf.exe.go":                                {},
		"src/math/fabsl.exe.go":                                {},
		"src/math/fdim.exe.go":                                 {},
		"src/math/fdimf.exe.go":                                {},
		"src/math/fdiml.exe.go":                                {},
		"src/math/fenv.exe.go":                                 {},
		"src/math/floor.exe.go":                                {},
		"src/math/floorf.exe.go":                               {},
		"src/math/floorl.exe.go":                               {},
		"src/math/fma.exe.go":                                  {},
		"src/math/fmaf.exe.go":                                 {},
		"src/math/fmal.exe.go":                                 {},
		"src/math/fmax.exe.go":                                 {},
		"src/math/fmaxf.exe.go":                                {},
		"src/math/fmaxl.exe.go":                                {},
		"src/math/fmin.exe.go":                                 {},
		"src/math/fminf.exe.go":                                {},
		"src/math/fminl.exe.go":                                {},
		"src/math/fmod.exe.go":                                 {},
		"src/math/fmodf.exe.go":                                {},
		"src/math/fmodl.exe.go":                                {},
		"src/math/frexp.exe.go":                                {},
		"src/math/frexpf.exe.go":                               {},
		"src/math/frexpl.exe.go":                               {},
		"src/math/hypot.exe.go":                                {},
		"src/math/hypotf.exe.go":                               {},
		"src/math/hypotl.exe.go":                               {},
		"src/math/ilogb.exe.go":                                {},
		"src/math/ilogbf.exe.go":                               {},
		"src/math/ilogbl.exe.go":                               {},
		"src/math/isless.exe.go":                               {},
		"src/math/j0.exe.go":                                   {},
		"src/math/j0f.exe.go":                                  {},
		"src/math/j1.exe.go":                                   {},
		"src/math/j1f.exe.go":                                  {},
		"src/math/jn.exe.go":                                   {},
		"src/math/jnf.exe.go":                                  {},
		"src/math/ldexp.exe.go":                                {},
		"src/math/ldexpf.exe.go":                               {},
		"src/math/ldexpl.exe.go":                               {},
		"src/math/lgamma.exe.go":                               {},
		"src/math/lgamma_r.exe.go":                             {},
		"src/math/lgammaf.exe.go":                              {},
		"src/math/lgammaf_r.exe.go":                            {},
		"src/math/lgammal.exe.go":                              {},
		"src/math/lgammal_r.exe.go":                            {},
		"src/math/llrint.exe.go":                               {},
		"src/math/llrintf.exe.go":                              {},
		"src/math/llrintl.exe.go":                              {},
		"src/math/llround.exe.go":                              {},
		"src/math/llroundf.exe.go":                             {},
		"src/math/llroundl.exe.go":                             {},
		"src/math/log.exe.go":                                  {},
		"src/math/log10.exe.go":                                {},
		"src/math/log10f.exe.go":                               {},
		"src/math/log10l.exe.go":                               {},
		"src/math/log1p.exe.go":                                {},
		"src/math/log1pf.exe.go":                               {},
		"src/math/log1pl.exe.go":                               {},
		"src/math/log2.exe.go":                                 {},
		"src/math/log2f.exe.go":                                {},
		"src/math/log2l.exe.go":                                {},
		"src/math/logb.exe.go":                                 {},
		"src/math/logbf.exe.go":                                {},
		"src/math/logbl.exe.go":                                {},
		"src/math/logf.exe.go":                                 {},
		"src/math/logl.exe.go":                                 {},
		"src/math/lrint.exe.go":                                {},
		"src/math/lrintf.exe.go":                               {},
		"src/math/lrintl.exe.go":                               {},
		"src/math/lround.exe.go":                               {},
		"src/math/lroundf.exe.go":                              {},
		"src/math/lroundl.exe.go":                              {},
		"src/math/modf.exe.go":                                 {},
		"src/math/modff.exe.go":                                {},
		"src/math/modfl.exe.go":                                {},
		"src/math/nearbyint.exe.go":                            {},
		"src/math/nearbyintf.exe.go":                           {},
		"src/math/nearbyintl.exe.go":                           {},
		"src/math/nextafter.exe.go":                            {},
		"src/math/nextafterf.exe.go":                           {},
		"src/math/nextafterl.exe.go":                           {},
		"src/math/nexttoward.exe.go":                           {},
		"src/math/nexttowardf.exe.go":                          {},
		"src/math/nexttowardl.exe.go":                          {},
		"src/math/pow.exe.go":                                  {},
		"src/math/pow10.exe.go":                                {},
		"src/math/pow10f.exe.go":                               {},
		"src/math/pow10l.exe.go":                               {},
		"src/math/powf.exe.go":                                 {},
		"src/math/powl.exe.go":                                 {},
		"src/math/remainder.exe.go":                            {},
		"src/math/remainderf.exe.go":                           {},
		"src/math/remainderl.exe.go":                           {},
		"src/math/remquo.exe.go":                               {},
		"src/math/remquof.exe.go":                              {},
		"src/math/remquol.exe.go":                              {},
		"src/math/rint.exe.go":                                 {},
		"src/math/rintf.exe.go":                                {},
		"src/math/rintl.exe.go":                                {},
		"src/math/round.exe.go":                                {},
		"src/math/roundf.exe.go":                               {},
		"src/math/roundl.exe.go":                               {},
		"src/math/scalb.exe.go":                                {},
		"src/math/scalbf.exe.go":                               {},
		"src/math/scalbln.exe.go":                              {},
		"src/math/scalblnf.exe.go":                             {},
		"src/math/scalblnl.exe.go":                             {},
		"src/math/scalbn.exe.go":                               {},
		"src/math/scalbnf.exe.go":                              {},
		"src/math/scalbnl.exe.go":                              {},
		"src/math/sin.exe.go":                                  {},
		"src/math/sincos.exe.go":                               {},
		"src/math/sincosf.exe.go":                              {},
		"src/math/sincosl.exe.go":                              {},
		"src/math/sinf.exe.go":                                 {},
		"src/math/sinh.exe.go":                                 {},
		"src/math/sinhf.exe.go":                                {},
		"src/math/sinhl.exe.go":                                {},
		"src/math/sinl.exe.go":                                 {},
		"src/math/sqrt.exe.go":                                 {},
		"src/math/sqrtf.exe.go":                                {},
		"src/math/sqrtl.exe.go":                                {},
		"src/math/tan.exe.go":                                  {},
		"src/math/tanf.exe.go":                                 {},
		"src/math/tanh.exe.go":                                 {},
		"src/math/tanhf.exe.go":                                {},
		"src/math/tanhl.exe.go":                                {},
		"src/math/tanl.exe.go":                                 {},
		"src/math/tgamma.exe.go":                               {},
		"src/math/tgammaf.exe.go":                              {},
		"src/math/tgammal.exe.go":                              {},
		"src/math/trunc.exe.go":                                {},
		"src/math/truncf.exe.go":                               {},
		"src/math/truncl.exe.go":                               {},
		"src/math/y0.exe.go":                                   {},
		"src/math/y0f.exe.go":                                  {},
		"src/math/y1.exe.go":                                   {},
		"src/math/y1f.exe.go":                                  {},
		"src/math/yn.exe.go":                                   {},
		"src/math/ynf.exe.go":                                  {},
		"src/regression/daemon-failure-static.exe.go":          {},
		"src/regression/daemon-failure.exe.go":                 {},
		"src/regression/pthread-robust-detach-static.exe.go":   {},
		"src/regression/pthread-robust-detach.exe.go":          {},
		"src/regression/pthread_cancel-sem_wait-static.exe.go": {},
		"src/regression/pthread_cancel-sem_wait.exe.go":        {},
		"src/regression/pthread_cond_wait-cancel_ignored-static.exe.go": {},
		"src/regression/pthread_cond_wait-cancel_ignored.exe.go":        {},
		"src/regression/pthread_condattr_setclock-static.exe.go":        {},
		"src/regression/pthread_condattr_setclock.exe.go":               {},
		"src/regression/pthread_once-deadlock-static.exe.go":            {},
		"src/regression/pthread_once-deadlock.exe.go":                   {},
		"src/regression/pthread_rwlock-ebusy-static.exe.go":             {},
		"src/regression/pthread_rwlock-ebusy.exe.go":                    {},
		"src/regression/raise-race-static.exe.go":                       {},
		"src/regression/raise-race.exe.go":                              {},
		"src/regression/sem_close-unmap-static.exe.go":                  {},
		"src/regression/sem_close-unmap.exe.go":                         {},
		"src/regression/tls_get_new-dtv.exe.go":                         {},

		//TODO EXEC FAIL
		"src/common/runtest.exe.go":                       {},
		"src/functional/dlopen.exe.go":                    {},
		"src/functional/popen-static.exe.go":              {},
		"src/functional/popen.exe.go":                     {},
		"src/functional/sscanf-static.exe.go":             {},
		"src/functional/sscanf.exe.go":                    {},
		"src/functional/strptime-static.exe.go":           {},
		"src/functional/strptime.exe.go":                  {},
		"src/functional/tgmath-static.exe.go":             {},
		"src/functional/tgmath.exe.go":                    {},
		"src/functional/tls_align-static.exe.go":          {},
		"src/functional/tls_init-static.exe.go":           {},
		"src/functional/tls_init.exe.go":                  {},
		"src/functional/tls_local_exec-static.exe.go":     {},
		"src/functional/tls_local_exec.exe.go":            {},
		"src/regression/malloc-brk-fail-static.exe.go":    {},
		"src/regression/malloc-brk-fail.exe.go":           {},
		"src/regression/malloc-oom-static.exe.go":         {},
		"src/regression/malloc-oom.exe.go":                {},
		"src/regression/pthread_create-oom-static.exe.go": {},
		"src/regression/pthread_create-oom.exe.go":        {},
		"src/regression/setenv-oom-static.exe.go":         {},
		"src/regression/setenv-oom.exe.go":                {},
		"src/regression/sigreturn-static.exe.go":          {},
		"src/regression/sigreturn.exe.go":                 {},
	},
	"linux/s390x": {
		"src/api/main.exe.go":                                           {},
		"src/functional/pthread_cancel-points-static.exe.go":            {},
		"src/functional/pthread_cancel-points.exe.go":                   {},
		"src/functional/pthread_cancel-static.exe.go":                   {},
		"src/functional/pthread_cancel.exe.go":                          {},
		"src/functional/pthread_mutex-static.exe.go":                    {},
		"src/functional/pthread_mutex.exe.go":                           {},
		"src/functional/pthread_mutex_pi-static.exe.go":                 {},
		"src/functional/pthread_mutex_pi.exe.go":                        {},
		"src/functional/pthread_robust-static.exe.go":                   {},
		"src/functional/pthread_robust.exe.go":                          {},
		"src/functional/sem_init-static.exe.go":                         {},
		"src/functional/sem_init.exe.go":                                {},
		"src/functional/sem_open-static.exe.go":                         {},
		"src/functional/sem_open.exe.go":                                {},
		"src/functional/setjmp-static.exe.go":                           {},
		"src/functional/setjmp.exe.go":                                  {},
		"src/functional/spawn-static.exe.go":                            {},
		"src/functional/spawn.exe.go":                                   {},
		"src/math/atanl.exe.go":                                         {},
		"src/math/cos.exe.go":                                           {},
		"src/math/cosl.exe.go":                                          {},
		"src/math/exp.exe.go":                                           {},
		"src/math/expl.exe.go":                                          {},
		"src/math/fenv.exe.go":                                          {},
		"src/math/fmaf.exe.go":                                          {},
		"src/math/nearbyint.exe.go":                                     {},
		"src/math/nearbyintf.exe.go":                                    {},
		"src/math/nearbyintl.exe.go":                                    {},
		"src/math/pow.exe.go":                                           {},
		"src/math/powl.exe.go":                                          {},
		"src/math/sin.exe.go":                                           {},
		"src/math/sinl.exe.go":                                          {},
		"src/math/tan.exe.go":                                           {},
		"src/math/tanl.exe.go":                                          {},
		"src/regression/daemon-failure-static.exe.go":                   {},
		"src/regression/daemon-failure.exe.go":                          {},
		"src/regression/pthread-robust-detach-static.exe.go":            {},
		"src/regression/pthread-robust-detach.exe.go":                   {},
		"src/regression/pthread_cancel-sem_wait-static.exe.go":          {},
		"src/regression/pthread_cancel-sem_wait.exe.go":                 {},
		"src/regression/pthread_cond_wait-cancel_ignored-static.exe.go": {},
		"src/regression/pthread_cond_wait-cancel_ignored.exe.go":        {},
		"src/regression/pthread_condattr_setclock-static.exe.go":        {},
		"src/regression/pthread_condattr_setclock.exe.go":               {},
		"src/regression/pthread_once-deadlock-static.exe.go":            {},
		"src/regression/pthread_once-deadlock.exe.go":                   {},
		"src/regression/pthread_rwlock-ebusy-static.exe.go":             {},
		"src/regression/pthread_rwlock-ebusy.exe.go":                    {},
		"src/regression/raise-race-static.exe.go":                       {},
		"src/regression/raise-race.exe.go":                              {},
		"src/regression/sem_close-unmap-static.exe.go":                  {},
		"src/regression/sem_close-unmap.exe.go":                         {},
		"src/regression/tls_get_new-dtv.exe.go":                         {},

		//TODO EXEC FAIL
		"src/common/runtest.exe.go":                       {},
		"src/functional/dlopen.exe.go":                    {},
		"src/functional/popen-static.exe.go":              {},
		"src/functional/popen.exe.go":                     {},
		"src/functional/sscanf-static.exe.go":             {},
		"src/functional/sscanf.exe.go":                    {},
		"src/functional/strptime-static.exe.go":           {},
		"src/functional/strptime.exe.go":                  {},
		"src/functional/tgmath-static.exe.go":             {},
		"src/functional/tgmath.exe.go":                    {},
		"src/functional/tls_align-static.exe.go":          {},
		"src/functional/tls_init-static.exe.go":           {},
		"src/functional/tls_init.exe.go":                  {},
		"src/functional/tls_local_exec-static.exe.go":     {},
		"src/functional/tls_local_exec.exe.go":            {},
		"src/regression/malloc-brk-fail-static.exe.go":    {},
		"src/regression/malloc-brk-fail.exe.go":           {},
		"src/regression/pthread_create-oom-static.exe.go": {},
		"src/regression/pthread_create-oom.exe.go":        {},
		"src/regression/setenv-oom-static.exe.go":         {},
		"src/regression/setenv-oom.exe.go":                {},
		"src/regression/sigreturn-static.exe.go":          {},
		"src/regression/sigreturn.exe.go":                 {},
	},
	"linux/ppc64le": {
		"src/api/main.exe.go":                                  {},
		"src/functional/pthread_cancel-points-static.exe.go":   {},
		"src/functional/pthread_cancel-points.exe.go":          {},
		"src/functional/pthread_cancel-static.exe.go":          {},
		"src/functional/pthread_cancel.exe.go":                 {},
		"src/functional/pthread_mutex-static.exe.go":           {},
		"src/functional/pthread_mutex.exe.go":                  {},
		"src/functional/pthread_mutex_pi-static.exe.go":        {},
		"src/functional/pthread_mutex_pi.exe.go":               {},
		"src/functional/pthread_robust-static.exe.go":          {},
		"src/functional/pthread_robust.exe.go":                 {},
		"src/functional/sem_init-static.exe.go":                {},
		"src/functional/sem_init.exe.go":                       {},
		"src/functional/sem_open-static.exe.go":                {},
		"src/functional/sem_open.exe.go":                       {},
		"src/functional/spawn-static.exe.go":                   {},
		"src/functional/spawn.exe.go":                          {},
		"src/math/acos.exe.go":                                 {},
		"src/math/acosf.exe.go":                                {},
		"src/math/acosh.exe.go":                                {},
		"src/math/acoshf.exe.go":                               {},
		"src/math/acoshl.exe.go":                               {},
		"src/math/acosl.exe.go":                                {},
		"src/math/asin.exe.go":                                 {},
		"src/math/asinf.exe.go":                                {},
		"src/math/asinh.exe.go":                                {},
		"src/math/asinhf.exe.go":                               {},
		"src/math/asinhl.exe.go":                               {},
		"src/math/asinl.exe.go":                                {},
		"src/math/atan.exe.go":                                 {},
		"src/math/atan2.exe.go":                                {},
		"src/math/atan2f.exe.go":                               {},
		"src/math/atan2l.exe.go":                               {},
		"src/math/atanf.exe.go":                                {},
		"src/math/atanh.exe.go":                                {},
		"src/math/atanhf.exe.go":                               {},
		"src/math/atanhl.exe.go":                               {},
		"src/math/atanl.exe.go":                                {},
		"src/math/cbrt.exe.go":                                 {},
		"src/math/cbrtf.exe.go":                                {},
		"src/math/cbrtl.exe.go":                                {},
		"src/math/ceil.exe.go":                                 {},
		"src/math/ceilf.exe.go":                                {},
		"src/math/ceill.exe.go":                                {},
		"src/math/copysign.exe.go":                             {},
		"src/math/copysignf.exe.go":                            {},
		"src/math/copysignl.exe.go":                            {},
		"src/math/cos.exe.go":                                  {},
		"src/math/cosf.exe.go":                                 {},
		"src/math/cosh.exe.go":                                 {},
		"src/math/coshf.exe.go":                                {},
		"src/math/coshl.exe.go":                                {},
		"src/math/cosl.exe.go":                                 {},
		"src/math/drem.exe.go":                                 {},
		"src/math/dremf.exe.go":                                {},
		"src/math/erf.exe.go":                                  {},
		"src/math/erfc.exe.go":                                 {},
		"src/math/erfcf.exe.go":                                {},
		"src/math/erfcl.exe.go":                                {},
		"src/math/erff.exe.go":                                 {},
		"src/math/erfl.exe.go":                                 {},
		"src/math/exp.exe.go":                                  {},
		"src/math/exp10.exe.go":                                {},
		"src/math/exp10f.exe.go":                               {},
		"src/math/exp10l.exe.go":                               {},
		"src/math/exp2.exe.go":                                 {},
		"src/math/exp2f.exe.go":                                {},
		"src/math/exp2l.exe.go":                                {},
		"src/math/expf.exe.go":                                 {},
		"src/math/expl.exe.go":                                 {},
		"src/math/expm1.exe.go":                                {},
		"src/math/expm1f.exe.go":                               {},
		"src/math/expm1l.exe.go":                               {},
		"src/math/fabs.exe.go":                                 {},
		"src/math/fabsf.exe.go":                                {},
		"src/math/fabsl.exe.go":                                {},
		"src/math/fdim.exe.go":                                 {},
		"src/math/fdimf.exe.go":                                {},
		"src/math/fdiml.exe.go":                                {},
		"src/math/fenv.exe.go":                                 {},
		"src/math/floor.exe.go":                                {},
		"src/math/floorf.exe.go":                               {},
		"src/math/floorl.exe.go":                               {},
		"src/math/fma.exe.go":                                  {},
		"src/math/fmaf.exe.go":                                 {},
		"src/math/fmal.exe.go":                                 {},
		"src/math/fmax.exe.go":                                 {},
		"src/math/fmaxf.exe.go":                                {},
		"src/math/fmaxl.exe.go":                                {},
		"src/math/fmin.exe.go":                                 {},
		"src/math/fminf.exe.go":                                {},
		"src/math/fminl.exe.go":                                {},
		"src/math/fmod.exe.go":                                 {},
		"src/math/fmodf.exe.go":                                {},
		"src/math/fmodl.exe.go":                                {},
		"src/math/frexp.exe.go":                                {},
		"src/math/frexpf.exe.go":                               {},
		"src/math/frexpl.exe.go":                               {},
		"src/math/hypot.exe.go":                                {},
		"src/math/hypotf.exe.go":                               {},
		"src/math/hypotl.exe.go":                               {},
		"src/math/ilogb.exe.go":                                {},
		"src/math/ilogbf.exe.go":                               {},
		"src/math/ilogbl.exe.go":                               {},
		"src/math/j0.exe.go":                                   {},
		"src/math/j0f.exe.go":                                  {},
		"src/math/j1.exe.go":                                   {},
		"src/math/j1f.exe.go":                                  {},
		"src/math/jn.exe.go":                                   {},
		"src/math/jnf.exe.go":                                  {},
		"src/math/ldexp.exe.go":                                {},
		"src/math/ldexpf.exe.go":                               {},
		"src/math/ldexpl.exe.go":                               {},
		"src/math/lgamma.exe.go":                               {},
		"src/math/lgamma_r.exe.go":                             {},
		"src/math/lgammaf.exe.go":                              {},
		"src/math/lgammaf_r.exe.go":                            {},
		"src/math/lgammal.exe.go":                              {},
		"src/math/lgammal_r.exe.go":                            {},
		"src/math/llrint.exe.go":                               {},
		"src/math/llrintf.exe.go":                              {},
		"src/math/llrintl.exe.go":                              {},
		"src/math/llround.exe.go":                              {},
		"src/math/llroundf.exe.go":                             {},
		"src/math/llroundl.exe.go":                             {},
		"src/math/log.exe.go":                                  {},
		"src/math/log10.exe.go":                                {},
		"src/math/log10f.exe.go":                               {},
		"src/math/log10l.exe.go":                               {},
		"src/math/log1p.exe.go":                                {},
		"src/math/log1pf.exe.go":                               {},
		"src/math/log1pl.exe.go":                               {},
		"src/math/log2.exe.go":                                 {},
		"src/math/log2f.exe.go":                                {},
		"src/math/log2l.exe.go":                                {},
		"src/math/logb.exe.go":                                 {},
		"src/math/logbf.exe.go":                                {},
		"src/math/logbl.exe.go":                                {},
		"src/math/logf.exe.go":                                 {},
		"src/math/logl.exe.go":                                 {},
		"src/math/lrint.exe.go":                                {},
		"src/math/lrintf.exe.go":                               {},
		"src/math/lrintl.exe.go":                               {},
		"src/math/lround.exe.go":                               {},
		"src/math/lroundf.exe.go":                              {},
		"src/math/lroundl.exe.go":                              {},
		"src/math/modf.exe.go":                                 {},
		"src/math/modff.exe.go":                                {},
		"src/math/modfl.exe.go":                                {},
		"src/math/nearbyint.exe.go":                            {},
		"src/math/nearbyintf.exe.go":                           {},
		"src/math/nearbyintl.exe.go":                           {},
		"src/math/nextafter.exe.go":                            {},
		"src/math/nextafterf.exe.go":                           {},
		"src/math/nextafterl.exe.go":                           {},
		"src/math/nexttoward.exe.go":                           {},
		"src/math/nexttowardf.exe.go":                          {},
		"src/math/nexttowardl.exe.go":                          {},
		"src/math/pow.exe.go":                                  {},
		"src/math/pow10.exe.go":                                {},
		"src/math/pow10f.exe.go":                               {},
		"src/math/pow10l.exe.go":                               {},
		"src/math/powf.exe.go":                                 {},
		"src/math/powl.exe.go":                                 {},
		"src/math/remainder.exe.go":                            {},
		"src/math/remainderf.exe.go":                           {},
		"src/math/remainderl.exe.go":                           {},
		"src/math/remquo.exe.go":                               {},
		"src/math/remquof.exe.go":                              {},
		"src/math/remquol.exe.go":                              {},
		"src/math/rint.exe.go":                                 {},
		"src/math/rintf.exe.go":                                {},
		"src/math/rintl.exe.go":                                {},
		"src/math/round.exe.go":                                {},
		"src/math/roundf.exe.go":                               {},
		"src/math/roundl.exe.go":                               {},
		"src/math/scalb.exe.go":                                {},
		"src/math/scalbf.exe.go":                               {},
		"src/math/scalbln.exe.go":                              {},
		"src/math/scalblnf.exe.go":                             {},
		"src/math/scalblnl.exe.go":                             {},
		"src/math/scalbn.exe.go":                               {},
		"src/math/scalbnf.exe.go":                              {},
		"src/math/scalbnl.exe.go":                              {},
		"src/math/sin.exe.go":                                  {},
		"src/math/sincos.exe.go":                               {},
		"src/math/sincosf.exe.go":                              {},
		"src/math/sincosl.exe.go":                              {},
		"src/math/sinf.exe.go":                                 {},
		"src/math/sinh.exe.go":                                 {},
		"src/math/sinhf.exe.go":                                {},
		"src/math/sinhl.exe.go":                                {},
		"src/math/sinl.exe.go":                                 {},
		"src/math/sqrt.exe.go":                                 {},
		"src/math/sqrtf.exe.go":                                {},
		"src/math/sqrtl.exe.go":                                {},
		"src/math/tan.exe.go":                                  {},
		"src/math/tanf.exe.go":                                 {},
		"src/math/tanh.exe.go":                                 {},
		"src/math/tanhf.exe.go":                                {},
		"src/math/tanhl.exe.go":                                {},
		"src/math/tanl.exe.go":                                 {},
		"src/math/tgamma.exe.go":                               {},
		"src/math/tgammaf.exe.go":                              {},
		"src/math/tgammal.exe.go":                              {},
		"src/math/trunc.exe.go":                                {},
		"src/math/truncf.exe.go":                               {},
		"src/math/truncl.exe.go":                               {},
		"src/math/y0.exe.go":                                   {},
		"src/math/y0f.exe.go":                                  {},
		"src/math/y1.exe.go":                                   {},
		"src/math/y1f.exe.go":                                  {},
		"src/math/yn.exe.go":                                   {},
		"src/math/ynf.exe.go":                                  {},
		"src/regression/daemon-failure-static.exe.go":          {},
		"src/regression/daemon-failure.exe.go":                 {},
		"src/regression/pthread-robust-detach-static.exe.go":   {},
		"src/regression/pthread-robust-detach.exe.go":          {},
		"src/regression/pthread_cancel-sem_wait-static.exe.go": {},
		"src/regression/pthread_cancel-sem_wait.exe.go":        {},
		"src/regression/pthread_cond_wait-cancel_ignored-static.exe.go": {},
		"src/regression/pthread_cond_wait-cancel_ignored.exe.go":        {},
		"src/regression/pthread_condattr_setclock-static.exe.go":        {},
		"src/regression/pthread_condattr_setclock.exe.go":               {},
		"src/regression/pthread_once-deadlock-static.exe.go":            {},
		"src/regression/pthread_once-deadlock.exe.go":                   {},
		"src/regression/pthread_rwlock-ebusy-static.exe.go":             {},
		"src/regression/pthread_rwlock-ebusy.exe.go":                    {},
		"src/regression/raise-race-static.exe.go":                       {},
		"src/regression/raise-race.exe.go":                              {},
		"src/regression/sem_close-unmap-static.exe.go":                  {},
		"src/regression/sem_close-unmap.exe.go":                         {},
		"src/regression/tls_get_new-dtv.exe.go":                         {},

		//TODO EXEC FAIL
		"src/common/runtest.exe.go":                       {},
		"src/functional/dlopen.exe.go":                    {},
		"src/functional/popen-static.exe.go":              {},
		"src/functional/popen.exe.go":                     {},
		"src/functional/setjmp-static.exe.go":             {},
		"src/functional/setjmp.exe.go":                    {},
		"src/functional/sscanf-static.exe.go":             {},
		"src/functional/sscanf.exe.go":                    {},
		"src/functional/strptime-static.exe.go":           {},
		"src/functional/strptime.exe.go":                  {},
		"src/functional/tgmath-static.exe.go":             {},
		"src/functional/tgmath.exe.go":                    {},
		"src/functional/tls_align-static.exe.go":          {},
		"src/functional/tls_init-static.exe.go":           {},
		"src/functional/tls_init.exe.go":                  {},
		"src/functional/tls_local_exec-static.exe.go":     {},
		"src/functional/tls_local_exec.exe.go":            {},
		"src/regression/malloc-brk-fail-static.exe.go":    {},
		"src/regression/malloc-brk-fail.exe.go":           {},
		"src/regression/malloc-oom-static.exe.go":         {},
		"src/regression/malloc-oom.exe.go":                {},
		"src/regression/pthread_create-oom-static.exe.go": {},
		"src/regression/pthread_create-oom.exe.go":        {},
		"src/regression/setenv-oom-static.exe.go":         {},
		"src/regression/setenv-oom.exe.go":                {},
		"src/regression/sigreturn-static.exe.go":          {},
		"src/regression/sigreturn.exe.go":                 {},
	},
	"linux/amd64": {
		"src/api/main.exe.go":                                  {},
		"src/functional/pthread_cancel-points-static.exe.go":   {},
		"src/functional/pthread_cancel-points.exe.go":          {},
		"src/functional/pthread_cancel-static.exe.go":          {},
		"src/functional/pthread_cancel.exe.go":                 {},
		"src/functional/pthread_mutex-static.exe.go":           {},
		"src/functional/pthread_mutex.exe.go":                  {},
		"src/functional/pthread_mutex_pi-static.exe.go":        {},
		"src/functional/pthread_mutex_pi.exe.go":               {},
		"src/functional/pthread_robust-static.exe.go":          {},
		"src/functional/pthread_robust.exe.go":                 {},
		"src/functional/sem_init-static.exe.go":                {},
		"src/functional/sem_init.exe.go":                       {},
		"src/functional/sem_open-static.exe.go":                {},
		"src/functional/sem_open.exe.go":                       {},
		"src/functional/setjmp-static.exe.go":                  {},
		"src/functional/setjmp.exe.go":                         {},
		"src/functional/spawn-static.exe.go":                   {},
		"src/functional/spawn.exe.go":                          {},
		"src/math/acos.exe.go":                                 {},
		"src/math/acosf.exe.go":                                {},
		"src/math/acosh.exe.go":                                {},
		"src/math/acoshf.exe.go":                               {},
		"src/math/acoshl.exe.go":                               {},
		"src/math/acosl.exe.go":                                {},
		"src/math/asin.exe.go":                                 {},
		"src/math/asinf.exe.go":                                {},
		"src/math/asinh.exe.go":                                {},
		"src/math/asinhf.exe.go":                               {},
		"src/math/asinhl.exe.go":                               {},
		"src/math/asinl.exe.go":                                {},
		"src/math/atan.exe.go":                                 {},
		"src/math/atan2.exe.go":                                {},
		"src/math/atan2f.exe.go":                               {},
		"src/math/atan2l.exe.go":                               {},
		"src/math/atanf.exe.go":                                {},
		"src/math/atanh.exe.go":                                {},
		"src/math/atanhf.exe.go":                               {},
		"src/math/atanhl.exe.go":                               {},
		"src/math/atanl.exe.go":                                {},
		"src/math/cbrt.exe.go":                                 {},
		"src/math/cbrtf.exe.go":                                {},
		"src/math/cbrtl.exe.go":                                {},
		"src/math/ceil.exe.go":                                 {},
		"src/math/ceilf.exe.go":                                {},
		"src/math/ceill.exe.go":                                {},
		"src/math/copysign.exe.go":                             {},
		"src/math/copysignf.exe.go":                            {},
		"src/math/copysignl.exe.go":                            {},
		"src/math/cos.exe.go":                                  {},
		"src/math/cosf.exe.go":                                 {},
		"src/math/cosh.exe.go":                                 {},
		"src/math/coshf.exe.go":                                {},
		"src/math/coshl.exe.go":                                {},
		"src/math/cosl.exe.go":                                 {},
		"src/math/drem.exe.go":                                 {},
		"src/math/dremf.exe.go":                                {},
		"src/math/erf.exe.go":                                  {},
		"src/math/erfc.exe.go":                                 {},
		"src/math/erfcf.exe.go":                                {},
		"src/math/erfcl.exe.go":                                {},
		"src/math/erff.exe.go":                                 {},
		"src/math/erfl.exe.go":                                 {},
		"src/math/exp.exe.go":                                  {},
		"src/math/exp10.exe.go":                                {},
		"src/math/exp10f.exe.go":                               {},
		"src/math/exp10l.exe.go":                               {},
		"src/math/exp2.exe.go":                                 {},
		"src/math/exp2f.exe.go":                                {},
		"src/math/exp2l.exe.go":                                {},
		"src/math/expf.exe.go":                                 {},
		"src/math/expl.exe.go":                                 {},
		"src/math/expm1.exe.go":                                {},
		"src/math/expm1f.exe.go":                               {},
		"src/math/expm1l.exe.go":                               {},
		"src/math/fabs.exe.go":                                 {},
		"src/math/fabsf.exe.go":                                {},
		"src/math/fabsl.exe.go":                                {},
		"src/math/fdim.exe.go":                                 {},
		"src/math/fdimf.exe.go":                                {},
		"src/math/fdiml.exe.go":                                {},
		"src/math/fenv.exe.go":                                 {},
		"src/math/floor.exe.go":                                {},
		"src/math/floorf.exe.go":                               {},
		"src/math/floorl.exe.go":                               {},
		"src/math/fma.exe.go":                                  {},
		"src/math/fmaf.exe.go":                                 {},
		"src/math/fmal.exe.go":                                 {},
		"src/math/fmax.exe.go":                                 {},
		"src/math/fmaxf.exe.go":                                {},
		"src/math/fmaxl.exe.go":                                {},
		"src/math/fmin.exe.go":                                 {},
		"src/math/fminf.exe.go":                                {},
		"src/math/fminl.exe.go":                                {},
		"src/math/fmod.exe.go":                                 {},
		"src/math/fmodf.exe.go":                                {},
		"src/math/fmodl.exe.go":                                {},
		"src/math/frexp.exe.go":                                {},
		"src/math/frexpf.exe.go":                               {},
		"src/math/frexpl.exe.go":                               {},
		"src/math/hypot.exe.go":                                {},
		"src/math/hypotf.exe.go":                               {},
		"src/math/hypotl.exe.go":                               {},
		"src/math/ilogb.exe.go":                                {},
		"src/math/ilogbf.exe.go":                               {},
		"src/math/ilogbl.exe.go":                               {},
		"src/math/j0.exe.go":                                   {},
		"src/math/j0f.exe.go":                                  {},
		"src/math/j1.exe.go":                                   {},
		"src/math/j1f.exe.go":                                  {},
		"src/math/jn.exe.go":                                   {},
		"src/math/jnf.exe.go":                                  {},
		"src/math/ldexp.exe.go":                                {},
		"src/math/ldexpf.exe.go":                               {},
		"src/math/ldexpl.exe.go":                               {},
		"src/math/lgamma.exe.go":                               {},
		"src/math/lgamma_r.exe.go":                             {},
		"src/math/lgammaf.exe.go":                              {},
		"src/math/lgammaf_r.exe.go":                            {},
		"src/math/lgammal.exe.go":                              {},
		"src/math/lgammal_r.exe.go":                            {},
		"src/math/llrint.exe.go":                               {},
		"src/math/llrintf.exe.go":                              {},
		"src/math/llrintl.exe.go":                              {},
		"src/math/llround.exe.go":                              {},
		"src/math/llroundf.exe.go":                             {},
		"src/math/llroundl.exe.go":                             {},
		"src/math/log.exe.go":                                  {},
		"src/math/log10.exe.go":                                {},
		"src/math/log10f.exe.go":                               {},
		"src/math/log10l.exe.go":                               {},
		"src/math/log1p.exe.go":                                {},
		"src/math/log1pf.exe.go":                               {},
		"src/math/log1pl.exe.go":                               {},
		"src/math/log2.exe.go":                                 {},
		"src/math/log2f.exe.go":                                {},
		"src/math/log2l.exe.go":                                {},
		"src/math/logb.exe.go":                                 {},
		"src/math/logbf.exe.go":                                {},
		"src/math/logbl.exe.go":                                {},
		"src/math/logf.exe.go":                                 {},
		"src/math/logl.exe.go":                                 {},
		"src/math/lrint.exe.go":                                {},
		"src/math/lrintf.exe.go":                               {},
		"src/math/lrintl.exe.go":                               {},
		"src/math/lround.exe.go":                               {},
		"src/math/lroundf.exe.go":                              {},
		"src/math/lroundl.exe.go":                              {},
		"src/math/modf.exe.go":                                 {},
		"src/math/modff.exe.go":                                {},
		"src/math/modfl.exe.go":                                {},
		"src/math/nearbyint.exe.go":                            {},
		"src/math/nearbyintf.exe.go":                           {},
		"src/math/nearbyintl.exe.go":                           {},
		"src/math/nextafter.exe.go":                            {},
		"src/math/nextafterf.exe.go":                           {},
		"src/math/nextafterl.exe.go":                           {},
		"src/math/nexttoward.exe.go":                           {},
		"src/math/nexttowardf.exe.go":                          {},
		"src/math/nexttowardl.exe.go":                          {},
		"src/math/pow.exe.go":                                  {},
		"src/math/pow10.exe.go":                                {},
		"src/math/pow10f.exe.go":                               {},
		"src/math/pow10l.exe.go":                               {},
		"src/math/powf.exe.go":                                 {},
		"src/math/powl.exe.go":                                 {},
		"src/math/remainder.exe.go":                            {},
		"src/math/remainderf.exe.go":                           {},
		"src/math/remainderl.exe.go":                           {},
		"src/math/remquo.exe.go":                               {},
		"src/math/remquof.exe.go":                              {},
		"src/math/remquol.exe.go":                              {},
		"src/math/rint.exe.go":                                 {},
		"src/math/rintf.exe.go":                                {},
		"src/math/rintl.exe.go":                                {},
		"src/math/round.exe.go":                                {},
		"src/math/roundf.exe.go":                               {},
		"src/math/roundl.exe.go":                               {},
		"src/math/scalb.exe.go":                                {},
		"src/math/scalbf.exe.go":                               {},
		"src/math/scalbln.exe.go":                              {},
		"src/math/scalblnf.exe.go":                             {},
		"src/math/scalblnl.exe.go":                             {},
		"src/math/scalbn.exe.go":                               {},
		"src/math/scalbnf.exe.go":                              {},
		"src/math/scalbnl.exe.go":                              {},
		"src/math/sin.exe.go":                                  {},
		"src/math/sincos.exe.go":                               {},
		"src/math/sincosf.exe.go":                              {},
		"src/math/sincosl.exe.go":                              {},
		"src/math/sinf.exe.go":                                 {},
		"src/math/sinh.exe.go":                                 {},
		"src/math/sinhf.exe.go":                                {},
		"src/math/sinhl.exe.go":                                {},
		"src/math/sinl.exe.go":                                 {},
		"src/math/sqrt.exe.go":                                 {},
		"src/math/sqrtf.exe.go":                                {},
		"src/math/sqrtl.exe.go":                                {},
		"src/math/tan.exe.go":                                  {},
		"src/math/tanf.exe.go":                                 {},
		"src/math/tanh.exe.go":                                 {},
		"src/math/tanhf.exe.go":                                {},
		"src/math/tanhl.exe.go":                                {},
		"src/math/tanl.exe.go":                                 {},
		"src/math/tgamma.exe.go":                               {},
		"src/math/tgammaf.exe.go":                              {},
		"src/math/tgammal.exe.go":                              {},
		"src/math/trunc.exe.go":                                {},
		"src/math/truncf.exe.go":                               {},
		"src/math/truncl.exe.go":                               {},
		"src/math/y0.exe.go":                                   {},
		"src/math/y0f.exe.go":                                  {},
		"src/math/y1.exe.go":                                   {},
		"src/math/y1f.exe.go":                                  {},
		"src/math/yn.exe.go":                                   {},
		"src/math/ynf.exe.go":                                  {},
		"src/regression/daemon-failure-static.exe.go":          {},
		"src/regression/daemon-failure.exe.go":                 {},
		"src/regression/pthread-robust-detach-static.exe.go":   {},
		"src/regression/pthread-robust-detach.exe.go":          {},
		"src/regression/pthread_cancel-sem_wait-static.exe.go": {},
		"src/regression/pthread_cancel-sem_wait.exe.go":        {},
		"src/regression/pthread_cond_wait-cancel_ignored-static.exe.go": {},
		"src/regression/pthread_cond_wait-cancel_ignored.exe.go":        {},
		"src/regression/pthread_condattr_setclock-static.exe.go":        {},
		"src/regression/pthread_condattr_setclock.exe.go":               {},
		"src/regression/pthread_once-deadlock-static.exe.go":            {},
		"src/regression/pthread_once-deadlock.exe.go":                   {},
		"src/regression/pthread_rwlock-ebusy-static.exe.go":             {},
		"src/regression/pthread_rwlock-ebusy.exe.go":                    {},
		"src/regression/raise-race-static.exe.go":                       {},
		"src/regression/raise-race.exe.go":                              {},
		"src/regression/sem_close-unmap-static.exe.go":                  {},
		"src/regression/sem_close-unmap.exe.go":                         {},
		"src/regression/tls_get_new-dtv.exe.go":                         {},

		//TODO EXEC FAIL
		"src/common/runtest.exe.go":                       {},
		"src/functional/dlopen.exe.go":                    {},
		"src/functional/popen-static.exe.go":              {},
		"src/functional/popen.exe.go":                     {},
		"src/functional/sscanf-static.exe.go":             {},
		"src/functional/sscanf.exe.go":                    {},
		"src/functional/strptime-static.exe.go":           {},
		"src/functional/strptime.exe.go":                  {},
		"src/functional/tgmath-static.exe.go":             {},
		"src/functional/tgmath.exe.go":                    {},
		"src/functional/tls_align-static.exe.go":          {},
		"src/functional/tls_init-static.exe.go":           {},
		"src/functional/tls_init.exe.go":                  {},
		"src/functional/tls_local_exec-static.exe.go":     {},
		"src/functional/tls_local_exec.exe.go":            {},
		"src/regression/malloc-brk-fail-static.exe.go":    {},
		"src/regression/malloc-brk-fail.exe.go":           {},
		"src/regression/malloc-oom-static.exe.go":         {},
		"src/regression/pthread_create-oom-static.exe.go": {},
		"src/regression/pthread_create-oom.exe.go":        {},
		"src/regression/setenv-oom-static.exe.go":         {},
		"src/regression/setenv-oom.exe.go":                {},
		"src/regression/sigreturn-static.exe.go":          {},
		"src/regression/sigreturn.exe.go":                 {},
	},
	"linux/loong64": {
		"src/api/main.exe.go":                                  {},
		"src/common/runtest.exe.go":                            {},
		"src/functional/basename-static.exe.go":                {},
		"src/functional/basename.exe.go":                       {},
		"src/functional/fwscanf-static.exe.go":                 {},
		"src/functional/fwscanf.exe.go":                        {},
		"src/functional/pthread_cancel-points-static.exe.go":   {},
		"src/functional/pthread_cancel-points.exe.go":          {},
		"src/functional/pthread_cancel-static.exe.go":          {},
		"src/functional/pthread_cancel.exe.go":                 {},
		"src/functional/pthread_mutex-static.exe.go":           {},
		"src/functional/pthread_mutex.exe.go":                  {},
		"src/functional/pthread_mutex_pi-static.exe.go":        {},
		"src/functional/pthread_mutex_pi.exe.go":               {},
		"src/functional/pthread_robust-static.exe.go":          {},
		"src/functional/pthread_robust.exe.go":                 {},
		"src/functional/sem_init-static.exe.go":                {},
		"src/functional/sem_init.exe.go":                       {},
		"src/functional/sem_open-static.exe.go":                {},
		"src/functional/sem_open.exe.go":                       {},
		"src/functional/setjmp-static.exe.go":                  {},
		"src/functional/setjmp.exe.go":                         {},
		"src/functional/spawn-static.exe.go":                   {},
		"src/functional/spawn.exe.go":                          {},
		"src/functional/sscanf-static.exe.go":                  {},
		"src/functional/sscanf.exe.go":                         {},
		"src/math/acos.exe.go":                                 {},
		"src/math/acosf.exe.go":                                {},
		"src/math/acosh.exe.go":                                {},
		"src/math/acoshf.exe.go":                               {},
		"src/math/acoshl.exe.go":                               {},
		"src/math/acosl.exe.go":                                {},
		"src/math/asin.exe.go":                                 {},
		"src/math/asinf.exe.go":                                {},
		"src/math/asinh.exe.go":                                {},
		"src/math/asinhf.exe.go":                               {},
		"src/math/asinhl.exe.go":                               {},
		"src/math/asinl.exe.go":                                {},
		"src/math/atan.exe.go":                                 {},
		"src/math/atan2.exe.go":                                {},
		"src/math/atan2f.exe.go":                               {},
		"src/math/atan2l.exe.go":                               {},
		"src/math/atanf.exe.go":                                {},
		"src/math/atanh.exe.go":                                {},
		"src/math/atanhf.exe.go":                               {},
		"src/math/atanhl.exe.go":                               {},
		"src/math/atanl.exe.go":                                {},
		"src/math/cbrt.exe.go":                                 {},
		"src/math/cbrtf.exe.go":                                {},
		"src/math/cbrtl.exe.go":                                {},
		"src/math/ceil.exe.go":                                 {},
		"src/math/ceilf.exe.go":                                {},
		"src/math/ceill.exe.go":                                {},
		"src/math/copysign.exe.go":                             {},
		"src/math/copysignf.exe.go":                            {},
		"src/math/copysignl.exe.go":                            {},
		"src/math/cos.exe.go":                                  {},
		"src/math/cosf.exe.go":                                 {},
		"src/math/cosh.exe.go":                                 {},
		"src/math/coshf.exe.go":                                {},
		"src/math/coshl.exe.go":                                {},
		"src/math/cosl.exe.go":                                 {},
		"src/math/drem.exe.go":                                 {},
		"src/math/dremf.exe.go":                                {},
		"src/math/erf.exe.go":                                  {},
		"src/math/erfc.exe.go":                                 {},
		"src/math/erfcf.exe.go":                                {},
		"src/math/erfcl.exe.go":                                {},
		"src/math/erff.exe.go":                                 {},
		"src/math/erfl.exe.go":                                 {},
		"src/math/exp.exe.go":                                  {},
		"src/math/exp10.exe.go":                                {},
		"src/math/exp10f.exe.go":                               {},
		"src/math/exp10l.exe.go":                               {},
		"src/math/exp2.exe.go":                                 {},
		"src/math/exp2f.exe.go":                                {},
		"src/math/exp2l.exe.go":                                {},
		"src/math/expf.exe.go":                                 {},
		"src/math/expl.exe.go":                                 {},
		"src/math/expm1.exe.go":                                {},
		"src/math/expm1f.exe.go":                               {},
		"src/math/expm1l.exe.go":                               {},
		"src/math/fabs.exe.go":                                 {},
		"src/math/fabsf.exe.go":                                {},
		"src/math/fabsl.exe.go":                                {},
		"src/math/fdim.exe.go":                                 {},
		"src/math/fdimf.exe.go":                                {},
		"src/math/fdiml.exe.go":                                {},
		"src/math/fenv.exe.go":                                 {},
		"src/math/floor.exe.go":                                {},
		"src/math/floorf.exe.go":                               {},
		"src/math/floorl.exe.go":                               {},
		"src/math/fma.exe.go":                                  {},
		"src/math/fmaf.exe.go":                                 {},
		"src/math/fmal.exe.go":                                 {},
		"src/math/fmax.exe.go":                                 {},
		"src/math/fmaxf.exe.go":                                {},
		"src/math/fmaxl.exe.go":                                {},
		"src/math/fmin.exe.go":                                 {},
		"src/math/fminf.exe.go":                                {},
		"src/math/fminl.exe.go":                                {},
		"src/math/fmod.exe.go":                                 {},
		"src/math/fmodf.exe.go":                                {},
		"src/math/fmodl.exe.go":                                {},
		"src/math/frexp.exe.go":                                {},
		"src/math/frexpf.exe.go":                               {},
		"src/math/frexpl.exe.go":                               {},
		"src/math/hypot.exe.go":                                {},
		"src/math/hypotf.exe.go":                               {},
		"src/math/hypotl.exe.go":                               {},
		"src/math/ilogb.exe.go":                                {},
		"src/math/ilogbf.exe.go":                               {},
		"src/math/ilogbl.exe.go":                               {},
		"src/math/isless.exe.go":                               {},
		"src/math/j0.exe.go":                                   {},
		"src/math/j0f.exe.go":                                  {},
		"src/math/j1.exe.go":                                   {},
		"src/math/j1f.exe.go":                                  {},
		"src/math/jn.exe.go":                                   {},
		"src/math/jnf.exe.go":                                  {},
		"src/math/ldexp.exe.go":                                {},
		"src/math/ldexpf.exe.go":                               {},
		"src/math/ldexpl.exe.go":                               {},
		"src/math/lgamma.exe.go":                               {},
		"src/math/lgamma_r.exe.go":                             {},
		"src/math/lgammaf.exe.go":                              {},
		"src/math/lgammaf_r.exe.go":                            {},
		"src/math/lgammal.exe.go":                              {},
		"src/math/lgammal_r.exe.go":                            {},
		"src/math/llrint.exe.go":                               {},
		"src/math/llrintf.exe.go":                              {},
		"src/math/llrintl.exe.go":                              {},
		"src/math/llround.exe.go":                              {},
		"src/math/llroundf.exe.go":                             {},
		"src/math/llroundl.exe.go":                             {},
		"src/math/log.exe.go":                                  {},
		"src/math/log10.exe.go":                                {},
		"src/math/log10f.exe.go":                               {},
		"src/math/log10l.exe.go":                               {},
		"src/math/log1p.exe.go":                                {},
		"src/math/log1pf.exe.go":                               {},
		"src/math/log1pl.exe.go":                               {},
		"src/math/log2.exe.go":                                 {},
		"src/math/log2f.exe.go":                                {},
		"src/math/log2l.exe.go":                                {},
		"src/math/logb.exe.go":                                 {},
		"src/math/logbf.exe.go":                                {},
		"src/math/logbl.exe.go":                                {},
		"src/math/logf.exe.go":                                 {},
		"src/math/logl.exe.go":                                 {},
		"src/math/lrint.exe.go":                                {},
		"src/math/lrintf.exe.go":                               {},
		"src/math/lrintl.exe.go":                               {},
		"src/math/lround.exe.go":                               {},
		"src/math/lroundf.exe.go":                              {},
		"src/math/lroundl.exe.go":                              {},
		"src/math/modf.exe.go":                                 {},
		"src/math/modff.exe.go":                                {},
		"src/math/modfl.exe.go":                                {},
		"src/math/nearbyint.exe.go":                            {},
		"src/math/nearbyintf.exe.go":                           {},
		"src/math/nearbyintl.exe.go":                           {},
		"src/math/nextafter.exe.go":                            {},
		"src/math/nextafterf.exe.go":                           {},
		"src/math/nextafterl.exe.go":                           {},
		"src/math/nexttoward.exe.go":                           {},
		"src/math/nexttowardf.exe.go":                          {},
		"src/math/nexttowardl.exe.go":                          {},
		"src/math/pow.exe.go":                                  {},
		"src/math/pow10.exe.go":                                {},
		"src/math/pow10f.exe.go":                               {},
		"src/math/pow10l.exe.go":                               {},
		"src/math/powf.exe.go":                                 {},
		"src/math/powl.exe.go":                                 {},
		"src/math/remainder.exe.go":                            {},
		"src/math/remainderf.exe.go":                           {},
		"src/math/remainderl.exe.go":                           {},
		"src/math/remquo.exe.go":                               {},
		"src/math/remquof.exe.go":                              {},
		"src/math/remquol.exe.go":                              {},
		"src/math/rint.exe.go":                                 {},
		"src/math/rintf.exe.go":                                {},
		"src/math/rintl.exe.go":                                {},
		"src/math/round.exe.go":                                {},
		"src/math/roundf.exe.go":                               {},
		"src/math/roundl.exe.go":                               {},
		"src/math/scalb.exe.go":                                {},
		"src/math/scalbf.exe.go":                               {},
		"src/math/scalbln.exe.go":                              {},
		"src/math/scalblnf.exe.go":                             {},
		"src/math/scalblnl.exe.go":                             {},
		"src/math/scalbn.exe.go":                               {},
		"src/math/scalbnf.exe.go":                              {},
		"src/math/scalbnl.exe.go":                              {},
		"src/math/sin.exe.go":                                  {},
		"src/math/sincos.exe.go":                               {},
		"src/math/sincosf.exe.go":                              {},
		"src/math/sincosl.exe.go":                              {},
		"src/math/sinf.exe.go":                                 {},
		"src/math/sinh.exe.go":                                 {},
		"src/math/sinhf.exe.go":                                {},
		"src/math/sinhl.exe.go":                                {},
		"src/math/sinl.exe.go":                                 {},
		"src/math/sqrt.exe.go":                                 {},
		"src/math/sqrtf.exe.go":                                {},
		"src/math/sqrtl.exe.go":                                {},
		"src/math/tan.exe.go":                                  {},
		"src/math/tanf.exe.go":                                 {},
		"src/math/tanh.exe.go":                                 {},
		"src/math/tanhf.exe.go":                                {},
		"src/math/tanhl.exe.go":                                {},
		"src/math/tanl.exe.go":                                 {},
		"src/math/tgamma.exe.go":                               {},
		"src/math/tgammaf.exe.go":                              {},
		"src/math/tgammal.exe.go":                              {},
		"src/math/trunc.exe.go":                                {},
		"src/math/truncf.exe.go":                               {},
		"src/math/truncl.exe.go":                               {},
		"src/math/y0.exe.go":                                   {},
		"src/math/y0f.exe.go":                                  {},
		"src/math/y1.exe.go":                                   {},
		"src/math/y1f.exe.go":                                  {},
		"src/math/yn.exe.go":                                   {},
		"src/math/ynf.exe.go":                                  {},
		"src/regression/daemon-failure-static.exe.go":          {},
		"src/regression/daemon-failure.exe.go":                 {},
		"src/regression/pthread-robust-detach-static.exe.go":   {},
		"src/regression/pthread-robust-detach.exe.go":          {},
		"src/regression/pthread_cancel-sem_wait-static.exe.go": {},
		"src/regression/pthread_cancel-sem_wait.exe.go":        {},
		"src/regression/pthread_cond_wait-cancel_ignored-static.exe.go": {},
		"src/regression/pthread_cond_wait-cancel_ignored.exe.go":        {},
		"src/regression/pthread_condattr_setclock-static.exe.go":        {},
		"src/regression/pthread_condattr_setclock.exe.go":               {},
		"src/regression/pthread_once-deadlock-static.exe.go":            {},
		"src/regression/pthread_once-deadlock.exe.go":                   {},
		"src/regression/pthread_rwlock-ebusy-static.exe.go":             {},
		"src/regression/pthread_rwlock-ebusy.exe.go":                    {},
		"src/regression/raise-race-static.exe.go":                       {},
		"src/regression/raise-race.exe.go":                              {},
		"src/regression/sem_close-unmap-static.exe.go":                  {},
		"src/regression/sem_close-unmap.exe.go":                         {},
		"src/regression/sigprocmask-internal-static.exe.go":             {},
		"src/regression/sigprocmask-internal.exe.go":                    {},
		"src/regression/tls_get_new-dtv.exe.go":                         {},

		//TODO EXEC FAIL
		"src/functional/dlopen.exe.go":                    {},
		"src/functional/popen-static.exe.go":              {},
		"src/functional/popen.exe.go":                     {},
		"src/functional/strptime-static.exe.go":           {},
		"src/functional/strptime.exe.go":                  {},
		"src/functional/tgmath-static.exe.go":             {},
		"src/functional/tgmath.exe.go":                    {},
		"src/functional/tls_align-static.exe.go":          {},
		"src/functional/tls_init-static.exe.go":           {},
		"src/functional/tls_init.exe.go":                  {},
		"src/functional/tls_local_exec-static.exe.go":     {},
		"src/functional/tls_local_exec.exe.go":            {},
		"src/regression/malloc-brk-fail-static.exe.go":    {},
		"src/regression/malloc-brk-fail.exe.go":           {},
		"src/regression/malloc-oom-static.exe.go":         {},
		"src/regression/malloc-oom.exe.go":                {},
		"src/regression/pthread_create-oom-static.exe.go": {},
		"src/regression/pthread_create-oom.exe.go":        {},
		"src/regression/setenv-oom-static.exe.go":         {},
		"src/regression/setenv-oom.exe.go":                {},
		"src/regression/sigreturn-static.exe.go":          {},
		"src/regression/sigreturn.exe.go":                 {},
	},
	"linux/arm64": {
		"src/api/main.exe.go":                                  {},
		"src/functional/pthread_cancel-points-static.exe.go":   {},
		"src/functional/pthread_cancel-points.exe.go":          {},
		"src/functional/pthread_cancel-static.exe.go":          {},
		"src/functional/pthread_cancel.exe.go":                 {},
		"src/functional/pthread_mutex-static.exe.go":           {},
		"src/functional/pthread_mutex.exe.go":                  {},
		"src/functional/pthread_mutex_pi-static.exe.go":        {},
		"src/functional/pthread_mutex_pi.exe.go":               {},
		"src/functional/pthread_robust-static.exe.go":          {},
		"src/functional/pthread_robust.exe.go":                 {},
		"src/functional/sem_init-static.exe.go":                {},
		"src/functional/sem_init.exe.go":                       {},
		"src/functional/sem_open-static.exe.go":                {},
		"src/functional/sem_open.exe.go":                       {},
		"src/functional/setjmp-static.exe.go":                  {},
		"src/functional/setjmp.exe.go":                         {},
		"src/functional/spawn-static.exe.go":                   {},
		"src/functional/spawn.exe.go":                          {},
		"src/math/acos.exe.go":                                 {},
		"src/math/acosf.exe.go":                                {},
		"src/math/acosh.exe.go":                                {},
		"src/math/acoshf.exe.go":                               {},
		"src/math/acoshl.exe.go":                               {},
		"src/math/acosl.exe.go":                                {},
		"src/math/asin.exe.go":                                 {},
		"src/math/asinf.exe.go":                                {},
		"src/math/asinh.exe.go":                                {},
		"src/math/asinhf.exe.go":                               {},
		"src/math/asinhl.exe.go":                               {},
		"src/math/asinl.exe.go":                                {},
		"src/math/atan.exe.go":                                 {},
		"src/math/atan2.exe.go":                                {},
		"src/math/atan2f.exe.go":                               {},
		"src/math/atan2l.exe.go":                               {},
		"src/math/atanf.exe.go":                                {},
		"src/math/atanh.exe.go":                                {},
		"src/math/atanhf.exe.go":                               {},
		"src/math/atanhl.exe.go":                               {},
		"src/math/atanl.exe.go":                                {},
		"src/math/cbrt.exe.go":                                 {},
		"src/math/cbrtf.exe.go":                                {},
		"src/math/cbrtl.exe.go":                                {},
		"src/math/ceil.exe.go":                                 {},
		"src/math/ceilf.exe.go":                                {},
		"src/math/ceill.exe.go":                                {},
		"src/math/copysign.exe.go":                             {},
		"src/math/copysignf.exe.go":                            {},
		"src/math/copysignl.exe.go":                            {},
		"src/math/cos.exe.go":                                  {},
		"src/math/cosf.exe.go":                                 {},
		"src/math/cosh.exe.go":                                 {},
		"src/math/coshf.exe.go":                                {},
		"src/math/coshl.exe.go":                                {},
		"src/math/cosl.exe.go":                                 {},
		"src/math/drem.exe.go":                                 {},
		"src/math/dremf.exe.go":                                {},
		"src/math/erf.exe.go":                                  {},
		"src/math/erfc.exe.go":                                 {},
		"src/math/erfcf.exe.go":                                {},
		"src/math/erfcl.exe.go":                                {},
		"src/math/erff.exe.go":                                 {},
		"src/math/erfl.exe.go":                                 {},
		"src/math/exp.exe.go":                                  {},
		"src/math/exp10.exe.go":                                {},
		"src/math/exp10f.exe.go":                               {},
		"src/math/exp10l.exe.go":                               {},
		"src/math/exp2.exe.go":                                 {},
		"src/math/exp2f.exe.go":                                {},
		"src/math/exp2l.exe.go":                                {},
		"src/math/expf.exe.go":                                 {},
		"src/math/expl.exe.go":                                 {},
		"src/math/expm1.exe.go":                                {},
		"src/math/expm1f.exe.go":                               {},
		"src/math/expm1l.exe.go":                               {},
		"src/math/fabs.exe.go":                                 {},
		"src/math/fabsf.exe.go":                                {},
		"src/math/fabsl.exe.go":                                {},
		"src/math/fdim.exe.go":                                 {},
		"src/math/fdimf.exe.go":                                {},
		"src/math/fdiml.exe.go":                                {},
		"src/math/fenv.exe.go":                                 {},
		"src/math/floor.exe.go":                                {},
		"src/math/floorf.exe.go":                               {},
		"src/math/floorl.exe.go":                               {},
		"src/math/fma.exe.go":                                  {},
		"src/math/fmaf.exe.go":                                 {},
		"src/math/fmal.exe.go":                                 {},
		"src/math/fmax.exe.go":                                 {},
		"src/math/fmaxf.exe.go":                                {},
		"src/math/fmaxl.exe.go":                                {},
		"src/math/fmin.exe.go":                                 {},
		"src/math/fminf.exe.go":                                {},
		"src/math/fminl.exe.go":                                {},
		"src/math/fmod.exe.go":                                 {},
		"src/math/fmodf.exe.go":                                {},
		"src/math/fmodl.exe.go":                                {},
		"src/math/frexp.exe.go":                                {},
		"src/math/frexpf.exe.go":                               {},
		"src/math/frexpl.exe.go":                               {},
		"src/math/hypot.exe.go":                                {},
		"src/math/hypotf.exe.go":                               {},
		"src/math/hypotl.exe.go":                               {},
		"src/math/ilogb.exe.go":                                {},
		"src/math/ilogbf.exe.go":                               {},
		"src/math/ilogbl.exe.go":                               {},
		"src/math/j0.exe.go":                                   {},
		"src/math/j0f.exe.go":                                  {},
		"src/math/j1.exe.go":                                   {},
		"src/math/j1f.exe.go":                                  {},
		"src/math/jn.exe.go":                                   {},
		"src/math/jnf.exe.go":                                  {},
		"src/math/ldexp.exe.go":                                {},
		"src/math/ldexpf.exe.go":                               {},
		"src/math/ldexpl.exe.go":                               {},
		"src/math/lgamma.exe.go":                               {},
		"src/math/lgamma_r.exe.go":                             {},
		"src/math/lgammaf.exe.go":                              {},
		"src/math/lgammaf_r.exe.go":                            {},
		"src/math/lgammal.exe.go":                              {},
		"src/math/lgammal_r.exe.go":                            {},
		"src/math/llrint.exe.go":                               {},
		"src/math/llrintf.exe.go":                              {},
		"src/math/llrintl.exe.go":                              {},
		"src/math/llround.exe.go":                              {},
		"src/math/llroundf.exe.go":                             {},
		"src/math/llroundl.exe.go":                             {},
		"src/math/log.exe.go":                                  {},
		"src/math/log10.exe.go":                                {},
		"src/math/log10f.exe.go":                               {},
		"src/math/log10l.exe.go":                               {},
		"src/math/log1p.exe.go":                                {},
		"src/math/log1pf.exe.go":                               {},
		"src/math/log1pl.exe.go":                               {},
		"src/math/log2.exe.go":                                 {},
		"src/math/log2f.exe.go":                                {},
		"src/math/log2l.exe.go":                                {},
		"src/math/logb.exe.go":                                 {},
		"src/math/logbf.exe.go":                                {},
		"src/math/logbl.exe.go":                                {},
		"src/math/logf.exe.go":                                 {},
		"src/math/logl.exe.go":                                 {},
		"src/math/lrint.exe.go":                                {},
		"src/math/lrintf.exe.go":                               {},
		"src/math/lrintl.exe.go":                               {},
		"src/math/lround.exe.go":                               {},
		"src/math/lroundf.exe.go":                              {},
		"src/math/lroundl.exe.go":                              {},
		"src/math/modf.exe.go":                                 {},
		"src/math/modff.exe.go":                                {},
		"src/math/modfl.exe.go":                                {},
		"src/math/nearbyint.exe.go":                            {},
		"src/math/nearbyintf.exe.go":                           {},
		"src/math/nearbyintl.exe.go":                           {},
		"src/math/nextafter.exe.go":                            {},
		"src/math/nextafterf.exe.go":                           {},
		"src/math/nextafterl.exe.go":                           {},
		"src/math/nexttoward.exe.go":                           {},
		"src/math/nexttowardf.exe.go":                          {},
		"src/math/nexttowardl.exe.go":                          {},
		"src/math/pow.exe.go":                                  {},
		"src/math/pow10.exe.go":                                {},
		"src/math/pow10f.exe.go":                               {},
		"src/math/pow10l.exe.go":                               {},
		"src/math/powf.exe.go":                                 {},
		"src/math/powl.exe.go":                                 {},
		"src/math/remainder.exe.go":                            {},
		"src/math/remainderf.exe.go":                           {},
		"src/math/remainderl.exe.go":                           {},
		"src/math/remquo.exe.go":                               {},
		"src/math/remquof.exe.go":                              {},
		"src/math/remquol.exe.go":                              {},
		"src/math/rint.exe.go":                                 {},
		"src/math/rintf.exe.go":                                {},
		"src/math/rintl.exe.go":                                {},
		"src/math/round.exe.go":                                {},
		"src/math/roundf.exe.go":                               {},
		"src/math/roundl.exe.go":                               {},
		"src/math/scalb.exe.go":                                {},
		"src/math/scalbf.exe.go":                               {},
		"src/math/scalbln.exe.go":                              {},
		"src/math/scalblnf.exe.go":                             {},
		"src/math/scalblnl.exe.go":                             {},
		"src/math/scalbn.exe.go":                               {},
		"src/math/scalbnf.exe.go":                              {},
		"src/math/scalbnl.exe.go":                              {},
		"src/math/sin.exe.go":                                  {},
		"src/math/sincos.exe.go":                               {},
		"src/math/sincosf.exe.go":                              {},
		"src/math/sincosl.exe.go":                              {},
		"src/math/sinf.exe.go":                                 {},
		"src/math/sinh.exe.go":                                 {},
		"src/math/sinhf.exe.go":                                {},
		"src/math/sinhl.exe.go":                                {},
		"src/math/sinl.exe.go":                                 {},
		"src/math/sqrt.exe.go":                                 {},
		"src/math/sqrtf.exe.go":                                {},
		"src/math/sqrtl.exe.go":                                {},
		"src/math/tan.exe.go":                                  {},
		"src/math/tanf.exe.go":                                 {},
		"src/math/tanh.exe.go":                                 {},
		"src/math/tanhf.exe.go":                                {},
		"src/math/tanhl.exe.go":                                {},
		"src/math/tanl.exe.go":                                 {},
		"src/math/tgamma.exe.go":                               {},
		"src/math/tgammaf.exe.go":                              {},
		"src/math/tgammal.exe.go":                              {},
		"src/math/trunc.exe.go":                                {},
		"src/math/truncf.exe.go":                               {},
		"src/math/truncl.exe.go":                               {},
		"src/math/y0.exe.go":                                   {},
		"src/math/y0f.exe.go":                                  {},
		"src/math/y1.exe.go":                                   {},
		"src/math/y1f.exe.go":                                  {},
		"src/math/yn.exe.go":                                   {},
		"src/math/ynf.exe.go":                                  {},
		"src/regression/daemon-failure-static.exe.go":          {},
		"src/regression/daemon-failure.exe.go":                 {},
		"src/regression/pthread-robust-detach-static.exe.go":   {},
		"src/regression/pthread-robust-detach.exe.go":          {},
		"src/regression/pthread_cancel-sem_wait-static.exe.go": {},
		"src/regression/pthread_cancel-sem_wait.exe.go":        {},
		"src/regression/pthread_cond_wait-cancel_ignored-static.exe.go": {},
		"src/regression/pthread_cond_wait-cancel_ignored.exe.go":        {},
		"src/regression/pthread_condattr_setclock-static.exe.go":        {},
		"src/regression/pthread_condattr_setclock.exe.go":               {},
		"src/regression/pthread_once-deadlock-static.exe.go":            {},
		"src/regression/pthread_once-deadlock.exe.go":                   {},
		"src/regression/pthread_rwlock-ebusy-static.exe.go":             {},
		"src/regression/pthread_rwlock-ebusy.exe.go":                    {},
		"src/regression/raise-race-static.exe.go":                       {},
		"src/regression/raise-race.exe.go":                              {},
		"src/regression/sem_close-unmap-static.exe.go":                  {},
		"src/regression/sem_close-unmap.exe.go":                         {},
		"src/regression/tls_get_new-dtv.exe.go":                         {},

		//TODO EXEC FAIL
		"src/common/runtest.exe.go":                       {},
		"src/functional/dlopen.exe.go":                    {},
		"src/functional/popen-static.exe.go":              {},
		"src/functional/popen.exe.go":                     {},
		"src/functional/sscanf-static.exe.go":             {},
		"src/functional/sscanf.exe.go":                    {},
		"src/functional/strptime-static.exe.go":           {},
		"src/functional/strptime.exe.go":                  {},
		"src/functional/tgmath-static.exe.go":             {},
		"src/functional/tgmath.exe.go":                    {},
		"src/functional/tls_align-static.exe.go":          {},
		"src/functional/tls_init-static.exe.go":           {},
		"src/functional/tls_init.exe.go":                  {},
		"src/functional/tls_local_exec-static.exe.go":     {},
		"src/functional/tls_local_exec.exe.go":            {},
		"src/regression/malloc-brk-fail-static.exe.go":    {},
		"src/regression/malloc-brk-fail.exe.go":           {},
		"src/regression/malloc-oom-static.exe.go":         {},
		"src/regression/malloc-oom.exe.go":                {},
		"src/regression/pthread_create-oom-static.exe.go": {},
		"src/regression/pthread_create-oom.exe.go":        {},
		"src/regression/setenv-oom-static.exe.go":         {},
		"src/regression/setenv-oom.exe.go":                {},
		"src/regression/sigreturn-static.exe.go":          {},
		"src/regression/sigreturn.exe.go":                 {},
	},
}

func TestLibc(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}

	tempdir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	mustShell(t, 10*time.Minute, "sh", "-c", fmt.Sprintf("rm -rf %s", filepath.Join(tempdir, "*")))
	libcTest := filepath.Join(tempdir, "libc-test")
	mustCopyDir(t, libcTest, filepath.Join("testdata", "nsz.repo.hu", "libc-test"), nil)
	cwd := util.MustAbsCwd(true)
	mustInDir(t, libcTest, func() error {
		mustShell(t, 10*time.Minute, "go", "mod", "init", "example.com/libc_test")
		mustShell(t, 10*time.Minute, "go", "get", "modernc.org/libc@latest")
		mustShell(t, 10*time.Minute, "go", "work", "init")
		mustShell(t, 10*time.Minute, "go", "work", "use", ".", cwd)
		return nil
	})

	if err := ccgo.NewTask(
		goos, goarch,
		[]string{
			os.Args[0],
			"--prefix-field=F",
			"-Drestrict=",
			"-I", filepath.Join(libcTest, "src", "common"),
			"-extended-errors",
			"-full-paths",
			"-isystem", filepath.Join(cwd, "include", goos, goarch),
			"-nostdinc",
			"-positions",

			// keep last
			"-exec", "make", "-C", libcTest, "-j", j,
		},
		os.Stdout, os.Stderr,
		nil,
	).Exec(); err != nil {
		t.Fatal(err)
	}
	p := newParallel(t, cpus, blacklists[target])
	mustInDir(t, libcTest, func() (err error) {
		err = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() || !strings.HasSuffix(path, ".exe.go") {
				return nil
			}

			if re != nil && !re.MatchString(path) {
				return nil
			}

			p.start(path)
			return nil
		})
		p.wg.Wait()
		return err
	})
	slices.SortFunc(p.errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	for _, v := range p.errs {
		t.Error(v)
	}
	slices.Sort(p.passed)
	for _, v := range p.passed {
		t.Logf("PASS %s", v)
	}
	t.Logf(
		"files=%v buildFails=%v skip=%v execFails=%v pass=%v",
		p.files.Load(), p.buildFails.Load(), p.skip.Load(), p.execFails.Load(), p.pass.Load(),
	)
	//                   all_test.go:554:  files=476 fails=339                             ok=137
	//                   all_test.go:588:  files=476 buildFails=283          execFails=33 pass=160
	// 202402251734      all_test.go:589:  files=476 buildFails=281          execFails=27 pass=168
	// 202204251952      all_test.go:589:  files=476 buildFails=279          execFails=29 pass=168
	// 202402261543      all_test.go:589:  files=476 buildFails=273          execFails=31 pass=172
	// 202402261622      all_test.go:589:  files=476 buildFails=269          execFails=35 pass=172
	// 202402271156      all_test.go:589:  files=476 buildFails=269          execFails=31 pass=176
	// 202403041850 all_musl_test.go:640:  files=476 buildFails=256          execFails=34 pass=186
	// 202403042209 all_musl_test.go:640:  files=476 buildFails=244          execFails=35 pass=197
	// 202403051424 all_musl_test.go:650:  files=476 buildFails=244 skip=16  execFails=19 pass=197
	// 202403151750 all_musl_test.go:1213: files=476 buildFails=  0 skip=273 execFails= 0 pass=203
	// 202403211526 all_musl_test.go:1214: files=477 buildFails=  0 skip=274 execFails= 0 pass=203
	// 202504172309 all_musl_test.go:2613: files=477 buildFails=  0 skip=273 execFails= 0 pass=204

}
