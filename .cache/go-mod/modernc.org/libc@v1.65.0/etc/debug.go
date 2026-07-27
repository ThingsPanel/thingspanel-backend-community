// Copyright 2024 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build none
// +build none

//	go mod init example.com/debug
//	go mod tidy

// SQLite date4.test
//
//	if {$tcl_platform(os)=="Linux"} {
//	  set FMT {%d,%e,%F,%H,%k,%I,%l,%j,%m,%M,%u,%w,%W,%Y,%%,%P,%p}
//	} else {
//	  set FMT {%d,%e,%F,%H,%I,%j,%p,%R,%u,%w,%W,%%}
//	}
//	for {set i 0} {$i<=24854} {incr i} {
//	  set TS [expr {$i*86401}]
//	  do_execsql_test date4-$i {
//	    SELECT strftime($::FMT,$::TS,'unixepoch');
//	  } [list [strftime $FMT $TS]]
//	}
//
//	date4-7384... Ok
//	date4-7385... Ok
//	date4-7386... Ok
//	date4-7387... Ok
//	date4-7388...
//	! date4-7388 expected: [{25,25,1990-03-25,03, 3,03, 3,084,03,03,7,0,12,1990,%,am,AM}]
//	! date4-7388 got:      [{25,25,1990-03-25,02, 2,02, 2,084,03,03,7,0,12,1990,%,am,AM}]
//	date4-7389... Ok
//	date4-7390... Ok
//	date4-7391... Ok

//	**   %d  day of month
//	**   %e  Like %d, the day of the month as a decimal number, but a leading zero is replaced by a space.
//	**   %F  Equivalent to %Y-%m-%d
//	**   %f  ** fractional seconds  SS.SSS
//	**   %H  hour 00-24
//	**   %j  day of year 000-366
//	**   %J  ** julian day number
//	**   %m  month 01-12
//	**   %M  minute 00-59
//	**   %s  seconds since 1970-01-01
//	**   %S  seconds 00-59
//	**   %w  day of week 0-6  Sunday==0
//	**   %W  week of year 00-53
//	**   %Y  year 0000-9999
//	**   %%  %

package main

/*

#include <time.h>

*/
import "C"

import (
	"fmt"
	"os"
	"runtime"
	gotime "time"
	"unsafe"

	"modernc.org/libc"
	"modernc.org/libc/time"
)

const (
	bufsz  = 1000
	format = "%d,%e,%F,%H,%k,%I,%l,%j,%m,%M,%u,%w,%W,%Y,%%,%P,%p"
)

var (
	buf  [bufsz + 1]byte
	bufp = &buf
	_    = gotime.Hour
)

func main() {
	fmt.Printf("sizeof time_t=%v\n", C.sizeof_time_t)
	tls := libc.NewTLS()

	defer tls.Close()

	pt := new(time.Time_t)

	var p runtime.Pinner
	p.Pin(pt)

	defer p.Unpin()

	gf, _ := libc.CString(format)
	cf := C.CString(format)
	for i := 0; i <= 24854; i++ {
		*pt = time.Time_t(i * 86401)
		g := libc.Xgmtime(tls, uintptr(unsafe.Pointer(pt)))
		e := C.gmtime((*C.time_t)(unsafe.Pointer(pt)))
		sg, se := libc.GoString((*time.Tm)(unsafe.Pointer(g)).Ftm_zone), C.GoString(e.tm_zone)
		if sg == "UTC" {
			sg = "GMT"
		}
		if uint64((*time.Tm)(unsafe.Pointer(g)).Ftm_sec) != uint64(e.tm_sec) ||
			uint64((*time.Tm)(unsafe.Pointer(g)).Ftm_min) != uint64(e.tm_min) ||
			uint64((*time.Tm)(unsafe.Pointer(g)).Ftm_hour) != uint64(e.tm_hour) ||
			uint64((*time.Tm)(unsafe.Pointer(g)).Ftm_mon) != uint64(e.tm_mon) ||
			uint64((*time.Tm)(unsafe.Pointer(g)).Ftm_year) != uint64(e.tm_year) ||
			uint64((*time.Tm)(unsafe.Pointer(g)).Ftm_wday) != uint64(e.tm_wday) ||
			uint64((*time.Tm)(unsafe.Pointer(g)).Ftm_yday) != uint64(e.tm_yday) ||
			uint64((*time.Tm)(unsafe.Pointer(g)).Ftm_isdst) != uint64(e.tm_isdst) ||
			uint64((*time.Tm)(unsafe.Pointer(g)).Ftm_gmtoff) != uint64(e.tm_gmtoff) ||
			sg != se ||
			false {
			fmt.Printf("FAIL i=%v t=%v g=%+v %q e=%+v %q)\n", i, *pt, (*time.Tm)(unsafe.Pointer(g)), sg, e, se)
			os.Exit(1)
		}

		n1 := libc.Xstrftime(tls, uintptr(unsafe.Pointer(bufp)), bufsz, gf, g)
		sg = string(buf[:n1])
		n2 := C.strftime((*C.char)(unsafe.Pointer(bufp)), bufsz, cf, e)
		se = string(buf[:n2])
		if sg != se {
			fmt.Printf("FAIL i=%v t=%v\nsg=`%s`\nse=`%s`\n", i, *pt, sg, se)
			os.Exit(1)
		}
	}
}
