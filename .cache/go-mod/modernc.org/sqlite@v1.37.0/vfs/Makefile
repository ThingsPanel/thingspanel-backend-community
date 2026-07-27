# Copyright 2022 The CC Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

.PHONY: build_all_targetsclean edit editor tmp x

build_all_targets:
	GOOS=darwin GOARCH=amd64 go build -v ./...
	GOOS=darwin GOARCH=amd64 go test -c -o /dev/null
	GOOS=darwin GOARCH=arm64 go build -v ./...
	GOOS=darwin GOARCH=arm64 go test -c -o /dev/null
	GOOS=freebsd GOARCH=amd64 go build -v ./...
	GOOS=freebsd GOARCH=amd64 go test -c -o /dev/null
	GOOS=freebsd GOARCH=386 go build -v ./...
	GOOS=freebsd GOARCH=386 go test -c -o /dev/null
	GOOS=freebsd GOARCH=arm go build -v ./...
	GOOS=freebsd GOARCH=arm go test -c -o /dev/null
	GOOS=linux GOARCH=386 go build -v ./...
	GOOS=linux GOARCH=386 go test -c -o /dev/null
	GOOS=linux GOARCH=amd64 go build -v ./...
	GOOS=linux GOARCH=amd64 go test -c -o /dev/null
	GOOS=linux GOARCH=arm go build -v ./...
	GOOS=linux GOARCH=arm go test -c -o /dev/null
	GOOS=linux GOARCH=arm64 go build -v ./...
	GOOS=linux GOARCH=arm64 go test -c -o /dev/null
	GOOS=linux GOARCH=ppc64le go build -v ./...
	GOOS=linux GOARCH=ppc64le go test -c -o /dev/null
	GOOS=linux GOARCH=riscv64 go build -v ./...
	GOOS=linux GOARCH=riscv64 go test -c -o /dev/null
	GOOS=linux GOARCH=s390x go build -v ./...
	GOOS=linux GOARCH=s390x go test -c -o /dev/null
	GOOS=netbsd GOARCH=amd64 go build -v ./...
	GOOS=netbsd GOARCH=amd64 go test -c -o /dev/null
	GOOS=openbsd GOARCH=amd64 go build -v ./...
	GOOS=openbsd GOARCH=amd64 go test -c -o /dev/null
	GOOS=openbsd GOARCH=arm64 go build -v ./...
	GOOS=openbsd GOARCH=arm64 go test -c -o /dev/null
	# GOOS=windows GOARCH=386 go build -v ./...
	# GOOS=windows GOARCH=386 go test -c -o /dev/null
	GOOS=windows GOARCH=amd64 go build -v ./...
	GOOS=windows GOARCH=amd64 go test -c -o /dev/null
	GOOS=windows GOARCH=arm64 go build -v ./...
	GOOS=windows GOARCH=arm64 go test -c -o /dev/null
	echo done

clean:
	rm -f log-* cpu.test mem.test *.out
	go clean

edit:
	@touch log
	@if [ -f "Session.vim" ]; then gvim -S & else gvim -p Makefile *.c *.go & fi

FN=vfs_$(shell go env GOOS)_$(shell go env GOARCH).go

editor:
	ccgo -o $(FN) c/vfs.c -Isqlite-amalgamation-3450200 \
		-D SQLITE_OS_UNIX \
		-extended-errors \
		-hide=vfsAccess \
		-hide=vfsClose \
		-hide=vfsFileSize \
		-hide=vfsFullPathname \
		-hide=vfsOpen \
		-hide=vfsRead \
		-ignore-link-errors \
		-import modernc.org/sqlite/lib \
		--package-name vfs \
		--prefix-external X
	sed -i 's/\<sqlite3_snprintf\>/sqlite3.Xsqlite3_snprintf/' $(FN)
	sed -i 's/\<sqlite3_malloc\>/sqlite3.Xsqlite3_malloc/' $(FN)
	go build -v
	gofmt -l -s -w *.go
	go test -o /dev/null -c

