package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestOperatorCommandsRejectIncompleteInputBeforeDatabaseAccess(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "preview", args: []string{"email-import", "preview", "--tenant-id", "tenant-a"}, want: "email import preview arguments incomplete"},
		{name: "apply", args: []string{"email-import", "apply", "--tenant-id", "tenant-a", "--legacy-group-id", "legacy-a", "--notification-group-id", "native-a", "--group-revision", "2", "--expected-route-version", "0", "--idempotency-key", "bad"}, want: "email import apply arguments incomplete"},
		{name: "rollback", args: []string{"route", "rollback", "--deployment-id", "deploy-a"}, want: "route rollback arguments incomplete"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(test.args, &stdout, &stderr)
			if err == nil || err.Error() != test.want {
				t.Fatalf("unexpected validation result: err=%v", err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("invalid operator command produced output: %q", stdout.String())
			}
		})
	}
}

func TestOperatorIdempotencyKeyValidation(t *testing.T) {
	for _, value := range []string{"", " short ", "with\nnewline", strings.Repeat("x", 129)} {
		if validOperatorIdempotencyKey(value) {
			t.Fatalf("unsafe idempotency key accepted: %q", value)
		}
	}
	if !validOperatorIdempotencyKey("email-import-20261009-01") {
		t.Fatal("valid stable idempotency key rejected")
	}
}
