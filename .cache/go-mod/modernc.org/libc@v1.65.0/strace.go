// Copyright 2023 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore
// +build ignore

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func fail(rc int, msg string, args ...any) {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(fmt.Sprintf(msg, args...)))
	os.Exit(rc)
}

func main() {
	m, err := filepath.Glob("*.go")
	if err != nil {
		fail(1, "%s\n", err)
	}

	format := false
	for _, fn := range m {
		b, err := os.ReadFile(fn)
		if err != nil {
			fail(1, "%s\n", err)
		}

		a := strings.Split(string(b), "\n")
		w := false
		for i, v := range a {
			if strings.HasPrefix(v, "func X") &&
				strings.Contains(v, "*TLS") &&
				!strings.Contains(v, "struct") &&
				!strings.Contains(v, "interface{") &&
				!strings.HasSuffix(v, ",") {
				switch {
				case strings.Contains(v, "}"):
					x := strings.Index(v, "{")
					if x < 0 {
						panic(fmt.Errorf("%s: %q", fn, v))
					}

					h := v[:x+1]
					t := v[x+1:]
					ok, tl := traceLine(fn, h)
					if !ok {
						continue
					}

					a[i] = "\n\n" + h + "\n\t" + tl + "\n" + t
					w = true
					format = true
				default:
					if i+1 < len(a) && !strings.Contains(a[i+1], "__ccgo_strace") {
						ok, tl := traceLine(fn, v)
						if !ok {
							continue
						}

						a[i] += "\n\t" + tl
						w = true
						format = true
					}
				}
			}
		}
		if w {
			if err := os.WriteFile(fn, []byte(strings.Join(a, "\n")), 0660); err != nil {
				fail(1, "%s\n", err)
			}
		}
	}
	if format {
		if out, err := exec.Command("sh", "-c", "gofmt -w *.go").CombinedOutput(); err != nil {
			fail(1, "%s FAIL err=%v\n", out, err)
		}
	}
}

// func Xaio_fsync(tls *TLS, op int32, cb uintptr) (r int32) {
func traceLine(fn, s string) (ok bool, _ string) {
	ok = true
	var b strings.Builder
	parts := strings.Split(s, "(")
	for i, v := range parts {
		switch i {
		case 0:
			// "func Xaio_fsync"
		case 1:
			// "tls *TLS, op int32, cb uintptr) "
			a := strings.Split(v, ",") // ["tls *TLS" "op int32" "cb uintptr) "]
			b.WriteString(`if __ccgo_strace { trc("`)
			var vals []string
			for j, w := range a {
				w = strings.TrimSpace(w)
				if x := strings.Index(w, " "); x > 0 {
					w := w[:x]
					if w == "_" {
						ok = false
					}
					if j != 0 {
						b.WriteString(" ")
					}
					fmt.Fprintf(&b, "%s=%%v", w)
					vals = append(vals, w)
				}
			}
			fmt.Fprintf(&b, `, (%%v:)", %s, origin(2))`, strings.Join(vals, ", "))
		case 2:
			// "r int32) {"
			x := strings.Index(v, " ")
			if x < 1 {
				panic(fmt.Errorf("%s: %q", fn, s))
			}

			r := v[:x]
			fmt.Fprintf(&b, `; defer func() { trc("-> %%v", %s)}()`, r)
		}
	}
	b.WriteString("}")
	return ok, b.String()
}

func mustCopyDir(dst, src string, canOverwrite func(fn string, fi os.FileInfo) bool, srcNotExistsOk bool) (files int, bytes int64) {
	file, bytes, err := copyDir(dst, src, canOverwrite, srcNotExistsOk)
	if err != nil {
		fail(1, "%s\n", err)
	}

	return file, bytes
}

func copyDir(dst, src string, canOverwrite func(fn string, fi os.FileInfo) bool, srcNotExistsOk bool) (files int, bytes int64, rerr error) {
	dst = filepath.FromSlash(dst)
	src = filepath.FromSlash(src)
	si, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) && srcNotExistsOk {
			err = nil
		}
		return 0, 0, err
	}

	if !si.IsDir() {
		return 0, 0, fmt.Errorf("cannot copy a file: %s", src)
	}

	return files, bytes, filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("cannot evaluate symlink %s: %v", path, err)
			}

			if info, err = os.Stat(target); err != nil {
				return fmt.Errorf("cannot stat %s: %v", target, err)
			}

			if info.IsDir() {
				rel, err := filepath.Rel(src, path)
				if err != nil {
					return err
				}

				dst2 := filepath.Join(dst, rel)
				if err := os.MkdirAll(dst2, 0770); err != nil {
					return err
				}

				f, b, err := copyDir(dst2, target, canOverwrite, srcNotExistsOk)
				files += f
				bytes += b
				return err
			}

			path = target
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0770)
		}

		n, err := copyFile(filepath.Join(dst, rel), path, canOverwrite)
		if err != nil {
			return err
		}

		files++
		bytes += n
		return nil
	})
}

func mustCopyFile(dst, src string, canOverwrite func(fn string, fi os.FileInfo) bool) int64 {
	n, err := copyFile(dst, src, canOverwrite)
	if err != nil {
		fail(1, "%s\n", err)
	}

	return n
}

func copyFile(dst, src string, canOverwrite func(fn string, fi os.FileInfo) bool) (n int64, rerr error) {
	src = filepath.FromSlash(src)
	si, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if si.IsDir() {
		return 0, fmt.Errorf("cannot copy a directory: %s", src)
	}

	dst = filepath.FromSlash(dst)
	if si.Size() == 0 {
		return 0, os.Remove(dst)
	}

	dstDir := filepath.Dir(dst)
	di, err := os.Stat(dstDir)
	switch {
	case err != nil:
		if !os.IsNotExist(err) {
			return 0, err
		}

		if err := os.MkdirAll(dstDir, 0770); err != nil {
			return 0, err
		}
	case err == nil:
		if !di.IsDir() {
			return 0, fmt.Errorf("cannot create directory, file exists: %s", dst)
		}
	}

	di, err = os.Stat(dst)
	switch {
	case err != nil && !os.IsNotExist(err):
		return 0, err
	case err == nil:
		if di.IsDir() {
			return 0, fmt.Errorf("cannot overwite a directory: %s", dst)
		}

		if canOverwrite != nil && !canOverwrite(dst, di) {
			return 0, fmt.Errorf("cannot overwite: %s", dst)
		}
	}

	s, err := os.Open(src)
	if err != nil {
		return 0, err
	}

	defer s.Close()
	r := bufio.NewReader(s)

	d, err := os.Create(dst)

	defer func() {
		if err := d.Close(); err != nil && rerr == nil {
			rerr = err
			return
		}

		if err := os.Chmod(dst, si.Mode()); err != nil && rerr == nil {
			rerr = err
			return
		}

		if err := os.Chtimes(dst, si.ModTime(), si.ModTime()); err != nil && rerr == nil {
			rerr = err
			return
		}
	}()

	w := bufio.NewWriter(d)

	defer func() {
		if err := w.Flush(); err != nil && rerr == nil {
			rerr = err
		}
	}()

	return io.Copy(w, r)
}
