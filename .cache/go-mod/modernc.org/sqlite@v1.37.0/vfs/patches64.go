// Copyright 2022 The Sqlite Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !arm && !386
// +build !arm,!386

package vfs

import (
	"unsafe"

	"modernc.org/libc"
	sqlite3 "modernc.org/sqlite/lib"
)

func init() {
	*(*func(*libc.TLS, uintptr) int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&vfsio)) + 8)) = vfsClose
	*(*func(*libc.TLS, uintptr, uintptr, int32, sqlite_int64) int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&vfsio)) + 16)) = vfsRead
	*(*func(*libc.TLS, uintptr, uintptr) int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&vfsio)) + 48)) = vfsFileSize
	*(*func(*libc.TLS, uintptr, int32) int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&vfsio)) + 56)) = vfsLock
	*(*func(*libc.TLS, uintptr, int32) int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&vfsio)) + 64)) = vfsUnlock
	*(*func(*libc.TLS, uintptr, uintptr) int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&vfsio)) + 72)) = vfsCheckReservedLock
	*(*func(*libc.TLS, uintptr, int32, uintptr) int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&vfsio)) + 80)) = vfsFileControl
	*(*func(*libc.TLS, uintptr) int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&vfsio)) + 88)) = vfsSectorSize
	*(*func(*libc.TLS, uintptr) int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&vfsio)) + 96)) = vfsDeviceCharacteristics
}

func vfsFullPathname(tls *libc.TLS, pVfs uintptr, zPath uintptr, nPathOut int32, zPathOut uintptr) int32 {
	libc.Xstrncpy(tls, zPathOut, zPath, uint64(nPathOut))
	return sqlite3.SQLITE_OK
}
