module modernc.org/sqlite

go 1.23.0

require (
	github.com/google/pprof v0.0.0-20250317173921-a4b03ec1a45e
	golang.org/x/sys v0.31.0
	modernc.org/fileutil v1.3.0
	modernc.org/libc v1.62.1
	modernc.org/mathutil v1.7.1
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/exp v0.0.0-20250305212735-054e65f0b394 // indirect
	modernc.org/memory v1.9.1 // indirect
)

retract [v1.16.0, v1.17.2] // https://gitlab.com/cznic/sqlite/-/issues/100

retract v1.19.0 // module source tree too large (max size is 524288000 bytes)

retract v1.20.1 // https://gitlab.com/cznic/sqlite/-/issues/123

retract v1.29.4 // tagged accidentally w/o builders checking the commit

retract v1.33.0 // intended to resolve #177 but breaks clients

retract v1.34.3 // intended to resolve #199 but breaks clients, see #200, fix in 1fcc86e9
