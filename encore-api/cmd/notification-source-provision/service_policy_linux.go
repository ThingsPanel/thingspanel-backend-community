package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func requireRootPrivatePath(path string, allowMissing bool) error {
	reject := errors.New("renewal requires root-owned private paths")
	if os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return reject
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			if current == path && allowMissing && os.IsNotExist(err) {
				continue
			}
			return reject
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return reject
		}
		if current == path && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
			return reject
		}
		if current != path && !info.IsDir() {
			return reject
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}
