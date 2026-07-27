package spec

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const windowsOS = "windows"

func TestNormalizer_NormalizeURI(t *testing.T) {
	type testNormalizePathsTestCases []struct {
		refPath    string
		base       string
		expOutput  string
		windows    bool
		nonWindows bool
	}

	testCases := func() testNormalizePathsTestCases {
		return testNormalizePathsTestCases{
			{
				// http basePath, absolute refPath
				refPath:   "",
				base:      "http://www.example.com/base/path/swagger.json",
				expOutput: "http://www.example.com/base/path/swagger.json",
			},
			{
				// http basePath, absolute refPath
				refPath:   "#",
				base:      "http://www.example.com/base/path/swagger.json",
				expOutput: "http://www.example.com/base/path/swagger.json",
			},
			{
				// http basePath, absolute refPath
				refPath:   "#/definitions/Pet",
				base:      "http://www.example.com/base/path/swagger.json",
				expOutput: "http://www.example.com/base/path/swagger.json#/definitions/Pet",
			},
			{
				// http basePath, absolute refPath
				refPath:   "http://www.anotherexample.com/another/base/path/swagger.json#/definitions/Pet",
				base:      "http://www.example.com/base/path/swagger.json",
				expOutput: "http://www.anotherexample.com/another/base/path/swagger.json#/definitions/Pet",
			},
			{
				// http basePath, relative refPath
				refPath:   "another/base/path/swagger.json#/definitions/Pet",
				base:      "http://www.example.com/base/path/swagger.json",
				expOutput: "http://www.example.com/base/path/another/base/path/swagger.json#/definitions/Pet",
			},
			{
				// file basePath, absolute refPath, no fragment
				refPath:   "/another/base/path.json",
				base:      "/base/path.json",
				expOutput: "/another/base/path.json",
			},
			{
				// path clean
				refPath:   "/another///base//path.json/",
				base:      "/base/path.json",
				expOutput: "/another/base/path.json",
			},
			{
				// path clean edge case
				refPath:   "",
				base:      "/base/path.json",
				expOutput: "/base/path.json",
			},
			{
				// file basePath, absolute refPath
				refPath:   "/another/base/path.json#/definitions/Pet",
				base:      "/base/path.json",
				expOutput: "/another/base/path.json#/definitions/Pet",
			},
			{
				// file basePath, relative refPath
				refPath:   "another/base/path.json#/definitions/Pet",
				base:      "/base/path.json",
				expOutput: "/base/another/base/path.json#/definitions/Pet",
			},
			{
				// file basePath, relative refPath
				refPath:   "./another/base/path.json#/definitions/Pet",
				base:      "/base/path.json",
				expOutput: "/base/another/base/path.json#/definitions/Pet",
			},
			{
				refPath:   "another/base/path.json#/definitions/Pet",
				base:      "file:///base/path.json",
				expOutput: "file:///base/another/base/path.json#/definitions/Pet",
			},
			{
				refPath:   "/another/base/path.json#/definitions/Pet",
				base:      "https://www.example.com:8443//base/path.json",
				expOutput: "https://www.example.com:8443/another/base/path.json#/definitions/Pet",
			},
			{
				// params in base
				refPath:   "/another/base/path.json#/definitions/Pet",
				base:      "https://www.example.com:8443//base/path.json?raw=true",
				expOutput: "https://www.example.com:8443/another/base/path.json?raw=true#/definitions/Pet",
			},
			{
				// params in ref
				refPath:   "https://origin.com/another/file.json?raw=true",
				base:      "https://www.example.com:8443//base/path.json?raw=true",
				expOutput: "https://origin.com/another/file.json?raw=true",
			},
			{
				refPath:   "another/base/def.yaml#/definitions/Pet",
				base:      "file:///base/path.json",
				expOutput: "file:///base/another/base/def.yaml#/definitions/Pet",
			},
			{
				refPath:   "",
				base:      "file:///base/path.json",
				expOutput: "file:///base/path.json",
			},
			{
				refPath:   "#",
				base:      "file:///base/path.json",
				expOutput: "file:///base/path.json",
			},
			{
				refPath:   "../other/another.json#/definitions/X",
				base:      "file:///base/path.json",
				expOutput: "file:///other/another.json#/definitions/X",
			},
			{
				// invalid URI
				refPath:   "\x7f\x9a",
				base:      "file:///base/path.json",
				expOutput: "file:///base/path.json",
			},
			{
				// file basePath, absolute refPath, no fragment
				refPath:   `C:\another\base\path.json`,
				base:      `file:///c:/base/path.json`,
				expOutput: `file:///c:/another/base/path.json`,
				windows:   true,
			},
			{
				// file basePath, absolute refPath
				refPath:   `C:\another\Base\path.json#/definitions/Pet`,
				base:      `file:///c:/base/path.json`,
				expOutput: `file:///c:/another/base/path.json#/definitions/Pet`,
				windows:   true,
			},
			{
				// file basePath, relative refPath
				refPath:   `another\base\path.json#/definitions/Pet`,
				base:      `file:///c:/base/path.json`,
				expOutput: `file:///c:/base/another/base/path.json#/definitions/Pet`,
				windows:   true,
			},
			{
				// file basePath, relative refPath
				refPath:   `.\another\base\path.json#/definitions/Pet`,
				base:      `file:///c:/base/path.json`,
				expOutput: `file:///c:/base/another/base/path.json#/definitions/Pet`,
				windows:   true,
			},
			{
				refPath:   `\\host\share\another\base\path.json#/definitions/Pet`,
				base:      `file:///c:/base/path.json`,
				expOutput: `file://host/share/another/base/path.json#/definitions/Pet`,
				windows:   true,
			},
			{
				// repair URI
				refPath:   `file://E:\Base\sub\File.json`,
				base:      `file:///c:/base/path.json`,
				expOutput: `file:///e:/base/sub/file.json`,
				windows:   true,
			},
			{
				// case sensitivity on local paths only (1/4)
				// see note:
				refPath:   `Resources.yaml#/definitions/Pets`,
				base:      `file:///c:/base/Spec.json`,
				expOutput: `file:///c:/base/Resources.yaml#/definitions/Pets`,
				windows:   true,
			},
			{
				// case sensitivity on local paths only (2/4)
				refPath:    `Resources.yaml#/definitions/Pets`,
				base:       `file:///c:/base/Spec.json`,
				expOutput:  `file:///c:/base/Resources.yaml#/definitions/Pets`,
				nonWindows: true,
			},
			{
				// case sensitivity on local paths only (3/4)
				refPath:   `Resources.yaml#/definitions/Pets`,
				base:      `https://example.com//base/Spec.json`,
				expOutput: `https://example.com/base/Resources.yaml#/definitions/Pets`,
			},
		}
	}()

	for _, toPin := range testCases {
		testCase := toPin
		if testCase.windows && runtime.GOOS != windowsOS {
			continue
		}
		if testCase.nonWindows && runtime.GOOS == windowsOS {
			continue
		}
		t.Run(testCase.refPath, func(t *testing.T) {
			t.Parallel()
			out := normalizeURI(testCase.refPath, testCase.base)
			assert.Equalf(t, testCase.expOutput, out,
				"unexpected normalized URL with $ref %q and base %q", testCase.refPath, testCase.base)
		})
	}
}

func TestNormalizer_NormalizeBase(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	if runtime.GOOS == windowsOS {
		cwd = "/" + strings.ToLower(filepath.ToSlash(cwd))
	}

	for _, toPin := range []struct {
		Base, Expected string
		Windows        bool
		NonWindows     bool
	}{
		{
			Base:     "",
			Expected: "file://$cwd", // edge case: this won't work because a document is a file
		},
		{
			Base:     "#",
			Expected: "file://$cwd", // edge case: this won't work because a document is a file
		},
		{
			Base:     "\x7f\x9a",
			Expected: "file://$cwd", // edge case: invalid URI
		},
		{
			Base:     ".",
			Expected: "file://$cwd", // edge case: this won't work because a document is a file
		},
		{
			Base:     "https://user:password@www.example.com:123/base/sub/file.json",
			Expected: "https://user:password@www.example.com:123/base/sub/file.json",
		},
		{
			// irrelevant fragment: cleaned
			Base:     "http://www.anotherexample.com/another/base/path/swagger.json#/definitions/Pet",
			Expected: "http://www.anotherexample.com/another/base/path/swagger.json",
		},
		{
			Base:     "base/sub/file.json",
			Expected: "file://$cwd/base/sub/file.json",
		},
		{
			Base:     "./base/sub/file.json",
			Expected: "file://$cwd/base/sub/file.json",
		},
		{
			Base:     "file:/base/sub/file.json",
			Expected: "file:///base/sub/file.json",
		},
		{
			// funny scheme, no path
			Base:     "smb://host",
			Expected: "smb://host",
		},
		{
			// explicit scheme, with host and path
			Base:     "gs://bucket/folder/file.json",
			Expected: "gs://bucket/folder/file.json",
		},
		{
			// explicit file scheme, with host and path
			Base:     "file://folder/file.json",
			Expected: "file://folder/file.json",
		},
		{
			// path clean
			Base:     "file:///folder//subfolder///file.json/",
			Expected: "file:///folder/subfolder/file.json",
		},
		{
			// path clean
			Base:       "///folder//subfolder///file.json/",
			Expected:   "file:///folder/subfolder/file.json",
			NonWindows: true,
		},
		{
			// path clean
			Base:     "///folder//subfolder///file.json/",
			Expected: "file:///c:/folder/subfolder/file.json",
			Windows:  true,
		},
		{
			// relevant query param: kept
			Base:     "https:///host/base/sub/file.json?query=param",
			Expected: "https:///host/base/sub/file.json?query=param",
		},
		{
			// no host component, absolute path
			Base:     `file:/base/sub/file.json`,
			Expected: "file:///base/sub/file.json",
		},
		{
			// handling dots (1/6): dodgy specification - resolved to /
			Base:     `file:///.`,
			Expected: "file:///",
		},
		{
			// handling dots (2/6): valid, cleaned to /
			Base:       "/..",
			Expected:   "file:///",
			NonWindows: true,
		},
		{
			// handling dots (3/6): valid, cleaned to /c:/ on windows
			Base:     "/..",
			Expected: "file:///c:",
			Windows:  true,
		},
		{
			// handling dots (4/6): dodgy specification - resolved to /
			Base:     `file:/.`,
			Expected: "file:///",
		},
		{
			// handling dots (5/6): dodgy specification - resolved to /
			Base:     `file:/..`,
			Expected: "file:///",
		},
		{
			// handling dots (6/6)
			Base:     `..`,
			Expected: "file://$dir",
		},
		// non-windows case
		{
			Base:       "/base/sub/file.json",
			Expected:   "file:///base/sub/file.json",
			NonWindows: true,
		},
		{
			// irrelevant query param (local file resolver): cleaned
			Base:       "/base/sub/file.json?query=param",
			Expected:   "file:///base/sub/file.json",
			NonWindows: true,
		},
		// windows-only cases
		{
			Base:     "/base/sub/file.json",
			Expected: "file:///c:/base/sub/file.json", // on windows, filepath.Abs("/a/b") prepends the "c:" drive
			Windows:  true,
		},
		{
			// case sensitivity
			Base:     `C:\Base\sub\File.json`,
			Expected: "file:///c:/base/sub/file.json",
			Windows:  true,
		},
		{
			// This one is parsed correctly: notice the third slash
			Base:     `file:///\Base\sub\File.json`,
			Expected: "file:///base/sub/file.json",
			Windows:  true,
		},
		{
			// absolute path
			Base:     `file:/\Base\sub\File.json`,
			Expected: "file:///base/sub/file.json",
			Windows:  true,
		},
		{
			// windows UNC path, no drive
			Base:     `\\host\share@1234\Folder\File.json`,
			Expected: "file://host/share@1234/folder/file.json",
			Windows:  true,
		},
		{
			// repair invalid use of leading "." on windows
			Base:     `file:///.\Base\sub\File.json`,
			Expected: "file://$cwd/base/sub/file.json",
			Windows:  true,
		},
		{
			Base:     `file:/E:\Base\sub\File.json`,
			Expected: "file:///e:/base/sub/file.json",
			Windows:  true,
		},
		{
			// repair URI (windows)
			// This one exhibits an example of invalid URI (missing a 3rd "/")
			Base:     `file://E:\Base\sub\File.json`,
			Expected: "file:///e:/base/sub/file.json",
			Windows:  true,
		},
	} {
		testCase := toPin
		if testCase.Windows && runtime.GOOS != windowsOS {
			continue
		}
		if testCase.NonWindows && runtime.GOOS == windowsOS {
			continue
		}
		t.Run(testCase.Base, func(t *testing.T) {
			t.Parallel()
			expected := strings.ReplaceAll(strings.ReplaceAll(testCase.Expected, "$cwd", cwd), "$dir", path.Dir(cwd))
			require.Equalf(t, expected, normalizeBase(testCase.Base), "for base %q", testCase.Base)

			// check for idempotence
			require.Equalf(t, expected, normalizeBase(normalizeBase(testCase.Base)),
				"expected idempotent behavior on base %q", testCase.Base)
		})
	}
}

func TestNormalizer_Denormalize(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	for _, toPin := range []struct {
		OriginalBase, Ref, Expected, ID string
		Windows                         bool
		NonWindows                      bool
	}{
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "#/definitions/X",
			Expected:     "#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json#ignoredFragment",
			Ref:          "#/definitions/X",
			Expected:     "#/definitions/X",
		},
		{
			OriginalBase: "https://user:password@example.com/a/b/c/file.json",
			Ref:          "https://user:password@example.com/a/b/c/other.json#/definitions/X",
			Expected:     "other.json#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "file:///a/b/c/items.json#/definitions/X",
			Expected:     "items.json#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "https:///x/y/z/items.json#/definitions/X",
			Expected:     "https:///x/y/z/items.json#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "file:///a/b/c/file.json#/definitions/X",
			Expected:     "#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "file:///a/b/c/d/other.json#/definitions/X",
			Expected:     "d/other.json#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "file:///a/b/c/file.json#",
			Expected:     "",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "file:///a/b/c/d/file.json",
			Expected:     "d/file.json",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "file:///a/b/c/file.json",
			Expected:     "",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "file:///a/b/c/../other.json#/definitions/X",
			Expected:     "../other.json#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			Ref:          "file:///a/b/c/../../other.json#/definitions/X",
			Expected:     "../../other.json#/definitions/X",
		},
		{
			// we may end up in this situation following ../.. in paths
			OriginalBase: "file:///a1/b/c/file.json",
			Ref:          "file:///a2/b/c/file.json#/definitions/X",
			Expected:     "file:///a2/b/c/file.json#/definitions/X",
		},
		{
			OriginalBase: "file:///file.json",
			Ref:          "file:///file.json#/definitions/X",
			Expected:     "#/definitions/X",
		},
		{
			OriginalBase: "file://host/file.json",
			Ref:          "file://host/file.json#/definitions/X",
			Expected:     "#/definitions/X",
		},
		{
			OriginalBase: "file://host1/file.json",
			Ref:          "file://host2/file.json#/definitions/X",
			Expected:     "file://host2/file.json#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			ID:           "https://myschema/",
			Ref:          "file:///a/b/c/file.json#/definitions/X",
			Expected:     "#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			ID:           "https://myschema/",
			Ref:          "https://myschema#/definitions/X",
			Expected:     "#/definitions/X",
		},
		{
			OriginalBase: "file:///a/b/c/file.json",
			ID:           "https://myschema/",
			Ref:          "https://myschema#/definitions/X",
			Expected:     "#/definitions/X",
		},
		{
			OriginalBase: "file:///folder/file.json",
			ID:           "https://example.com/schema",
			Ref:          "https://example.com/schema#/definitions/X",
			Expected:     "#/definitions/X",
		},
		{
			OriginalBase: "file:///folder/file.json",
			ID:           "https://example.com/schema",
			Ref:          "https://example.com/schema/other-file.json#/definitions/X",
			Expected:     "https://example.com/other-file.json#/definitions/X",
		},
	} {
		testCase := toPin
		if testCase.Windows && runtime.GOOS != windowsOS { // windows only
			continue
		}
		if testCase.NonWindows && runtime.GOOS == windowsOS { // non-windows only
			continue
		}
		t.Run(testCase.Ref, func(t *testing.T) {
			t.Parallel()
			expected := strings.ReplaceAll(testCase.Expected, "$cwd", cwd)
			ref := MustCreateRef(testCase.Ref)
			newRef := denormalizeRef(&ref, testCase.OriginalBase, testCase.ID)
			require.NotNil(t, newRef)
			require.Equalf(t, expected, newRef.String(),
				"expected %s, but got %s", testCase.Expected, newRef.String())
		})
	}
}
