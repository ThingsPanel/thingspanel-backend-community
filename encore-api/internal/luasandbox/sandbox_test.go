package luasandbox

import (
	"context"
	"strings"
	"testing"
)

func run(script, input string) Result {
	return Run(context.Background(), script, input, "t/up")
}

func TestRunTransformsJSONWithTheDocumentedTemplate(t *testing.T) {
	got := run(`function encodeInp(msg, topic)
  local json = require("json")
  local t = json.decode(msg)
  t.current = t.current * 2
  t.topic = topic
  return json.encode(t)
end`, `{"current":21}`)
	if !got.Succeeded || got.Output != `{"current":42,"topic":"t/up"}` {
		t.Fatalf("result=%+v", got)
	}
}

func TestRunReportsStableErrorCodes(t *testing.T) {
	for name, tc := range map[string]struct{ script, code string }{
		"syntax":      {`function encodeInp(`, CodeSyntax},
		"runtime":     {`function encodeInp(m,t) error("boom") end`, CodeRuntime},
		"timeout":     {`function encodeInp(m,t) while true do end end`, CodeTimeout},
		"memory":      {`function encodeInp(m,t) local s="x" while true do s=s..s end end`, CodeMemory},
		"no entry":    {`x = 1`, CodeNoEntry},
		"bad return":  {`function encodeInp(m,t) return 5 end`, CodeBadReturn},
		"huge output": {`function encodeInp(m,t) return string.rep("a", 300000) end`, CodeOutputSize},
	} {
		got := run(tc.script, "{}")
		if got.Succeeded || got.ErrorCode != tc.code {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got := run(`function encodeInp(m,t) return m end`, strings.Repeat("a", 262145)); got.ErrorCode != CodeInputSize {
		t.Errorf("input size: %+v", got)
	}
}

func TestRunDeniesHostAccess(t *testing.T) {
	for name, script := range map[string]string{
		"os":        `function encodeInp(m,t) return os.getenv("HOME") end`,
		"io":        `function encodeInp(m,t) return io.open("/etc/passwd"):read("*a") end`,
		"dofile":    `function encodeInp(m,t) return dofile("/etc/passwd") end`,
		"load":      `function encodeInp(m,t) return load("return 1")() end`,
		"require":   `function encodeInp(m,t) return require("os").getenv("HOME") end`,
		"package":   `function encodeInp(m,t) return package.path end`,
		"debug":     `function encodeInp(m,t) return debug.traceback() end`,
		"setmeta g": `function encodeInp(m,t) return tostring(getfenv) end`,
	} {
		got := run(script, "{}")
		if got.Succeeded && got.Output != "nil" {
			t.Errorf("%s reached the host: %+v", name, got)
		}
	}
}
