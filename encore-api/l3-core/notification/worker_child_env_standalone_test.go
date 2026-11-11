//go:build !encore_app

package notification

import "os"

func workerChildEnvironment() []string { return os.Environ() }
