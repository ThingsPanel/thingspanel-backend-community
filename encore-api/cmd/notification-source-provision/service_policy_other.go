//go:build !linux

package main

import "errors"

func requireRootPrivatePath(string, bool) error {
	return errors.New("service renewal is available only to Linux deployment root")
}
