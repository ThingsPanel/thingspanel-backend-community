//go:build ignore
// +build ignore

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"modernc.org/cc/v3"
	ccgo "modernc.org/ccgo/v3/lib"
)

var (
	goos   = runtime.GOOS
	goarch = runtime.GOARCH
)

func fail(err error) {
	fmt.Fprintf(os.Stderr, "%v (%v: %v:)\n", err, origin(3), origin(2))
	os.Exit(1)
}

func main() {
	_, _, hostSysIncludes, err := cc.HostConfig(os.Getenv("CCGO_CPP"))
	if err != nil {
		fail(err)
	}

	if err := libcHeaders(hostSysIncludes); err != nil {
		fail(err)
	}
}

type echoWriter struct {
	w bytes.Buffer
}

func (w *echoWriter) Write(b []byte) (int, error) {
	os.Stdout.Write(b)
	return w.w.Write(b)
}

func runcc(args ...string) ([]byte, error) {
	args = append([]string{"ccgo"}, args...)
	// fmt.Printf("%q\n", args)
	var out echoWriter
	err := ccgo.NewTask(args, &out, &out).Main()
	return out.w.Bytes(), err
}

func libcHeaders(paths []string) error {
	const cfile = "gen.c"
	return filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			return nil
		}

		path = filepath.Clean(path)
		if strings.HasPrefix(path, ".") {
			return nil
		}

		dir := path
		ok := false
		for _, v := range paths {
			full := filepath.Join(v, dir+".h")
			if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
				ok = true
				break
			}
		}
		if !ok {
			return nil
		}

		var src string
		switch filepath.ToSlash(path) {
		case "fts":
			src = `
#include <sys/types.h>
#include <sys/stat.h>
#include <fts.h>
`
		default:
			src = fmt.Sprintf("#include <%s.h>\n", dir)
		}
		src += "static char _;\n"
		fn := filepath.Join(dir, cfile)
		if err := ioutil.WriteFile(fn, []byte(src), 0660); err != nil {
			return err
		}

		defer os.Remove(fn)

		dest := filepath.Join(path, fmt.Sprintf("%s_%s_%s.go", filepath.Base(path), goos, goarch))
		base := filepath.Base(dir)
		argv := []string{
			fn,

			"-crt-import-path", "",
			"-export-defines", "",
			"-export-enums", "",
			"-export-externs", "X",
			"-export-fields", "F",
			"-export-structs", "",
			"-export-typedefs", "",
			"-header",
			"-hide", "_OSSwapInt16,_OSSwapInt32,_OSSwapInt64",
			"-ignore-unsupported-alignment",
			"-o", dest,
			"-pkgname", base,
		}
		out, err := runcc(argv...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s%s\n", path, out, err)
		} else {
			fmt.Fprintf(os.Stdout, "%s\n%s", path, out)
		}
		return nil
	})
}

func origin(skip int) string {
	pc, fn, fl, _ := runtime.Caller(skip)
	f := runtime.FuncForPC(pc)
	var fns string
	if f != nil {
		fns = f.Name()
		if x := strings.LastIndex(fns, "."); x > 0 {
			fns = fns[x+1:]
		}
	}
	return fmt.Sprintf("%s:%d:%s", filepath.Base(fn), fl, fns)
}
