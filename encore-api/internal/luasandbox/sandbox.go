// Package luasandbox runs a template processing script against one sample
// message without any host access. It exists for the debug-run operation
// (testProcessingScript): the script gets the base, table, string and math
// libraries plus a small json module, and nothing that reaches the file
// system, network, process or other Lua chunks.
package luasandbox

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	// EngineVersion is reported to clients so results can be tied to the
	// limits below.
	EngineVersion = "gopher-lua-5.1/sandbox-v1"

	MaxRunTime     = time.Second
	MaxOutputBytes = 256 * 1024
	// maxHeapGrowth bounds how far the process heap may grow during one run.
	// gopher-lua's own SetMx exits the whole process when exceeded, so memory
	// is policed by watchHeap instead. The figure is process-wide and therefore
	// approximate: runs are serialized and garbage can trip it early.
	maxHeapGrowth    = 32 << 20
	heapPollInterval = 2 * time.Millisecond
	// maxInputBytes matches the request schema.
	maxInputBytes = 256 * 1024

	Entrypoint = "encodeInp"

	CodeSyntax     = "SCRIPT_SYNTAX_ERROR"
	CodeRuntime    = "SCRIPT_RUNTIME_ERROR"
	CodeTimeout    = "SCRIPT_TIMEOUT"
	CodeMemory     = "SCRIPT_MEMORY_LIMIT"
	CodeBadReturn  = "SCRIPT_BAD_RETURN"
	CodeNoEntry    = "SCRIPT_ENTRYPOINT_MISSING"
	CodeOutputSize = "SCRIPT_OUTPUT_TOO_LARGE"
	CodeInputSize  = "SCRIPT_INPUT_TOO_LARGE"
)

// Result is the outcome of one debug run. Failures are results, not errors:
// a script that does not compile or times out is an expected answer.
type Result struct {
	Succeeded bool
	Output    string
	ErrorCode string
	Message   string
	Duration  time.Duration
}

// runs serializes executions so the heap watchdog measures one script at a time.
var runs sync.Mutex

// watchHeap cancels the run when the heap grows past maxHeapGrowth. It returns
// a stop function and a flag that reports whether the limit fired.
func watchHeap(cancel context.CancelFunc) (stop func(), exceeded *atomic.Bool) {
	exceeded = new(atomic.Bool)
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(heapPollInterval)
		defer ticker.Stop()
		var now runtime.MemStats
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				runtime.ReadMemStats(&now)
				if now.HeapAlloc > base.HeapAlloc && now.HeapAlloc-base.HeapAlloc > maxHeapGrowth {
					exceeded.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	return func() { close(done); <-finished }, exceeded
}

// Run executes the script's encodeInp(msg, topic) once.
func Run(parent context.Context, script, input, topic string) Result {
	runs.Lock()
	defer runs.Unlock()
	started := time.Now()
	fail := func(code, message string) Result {
		return Result{ErrorCode: code, Message: truncate(message, 2048), Duration: time.Since(started)}
	}
	if len(input) > maxInputBytes {
		return fail(CodeInputSize, "input exceeds the 256 KiB limit")
	}
	ctx, cancel := context.WithTimeout(parent, MaxRunTime)
	defer cancel()
	stopWatch, overMemory := watchHeap(cancel)
	defer stopWatch()

	L := lua.NewState(lua.Options{SkipOpenLibs: true, RegistrySize: 1024 * 8, RegistryMaxSize: 1024 * 64, CallStackSize: 200})
	defer L.Close()
	L.SetContext(ctx)
	openSafeLibs(L)

	fn, err := L.LoadString(script)
	if err != nil {
		return fail(CodeSyntax, err.Error())
	}
	L.Push(fn)
	if err := L.PCall(0, 0, nil); err != nil {
		return fail(classify(ctx, overMemory, err), err.Error())
	}
	entry := L.GetGlobal(Entrypoint)
	if entry.Type() != lua.LTFunction {
		return fail(CodeNoEntry, "the script does not define a function named "+Entrypoint)
	}
	if err := L.CallByParam(lua.P{Fn: entry, NRet: 1, Protect: true}, lua.LString(input), lua.LString(topic)); err != nil {
		return fail(classify(ctx, overMemory, err), err.Error())
	}
	result := L.Get(-1)
	text, ok := result.(lua.LString)
	if !ok {
		return fail(CodeBadReturn, "encodeInp must return a string, got "+result.Type().String())
	}
	if len(text) > MaxOutputBytes {
		return fail(CodeOutputSize, "output exceeds the 256 KiB limit")
	}
	return Result{Succeeded: true, Output: string(text), Duration: time.Since(started)}
}

func classify(ctx context.Context, overMemory *atomic.Bool, err error) string {
	if overMemory.Load() {
		return CodeMemory
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
		return CodeTimeout
	}
	return CodeRuntime
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func openSafeLibs(L *lua.LState) {
	for _, lib := range []struct {
		name string
		open lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(lib.open))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	// The base library brings loaders and file access; remove all of them.
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "require", "module", "collectgarbage", "getfenv", "setfenv", "newproxy", "print"} {
		L.SetGlobal(name, lua.LNil)
	}
	json := L.NewTable()
	L.SetFuncs(json, map[string]lua.LGFunction{"decode": jsonDecode, "encode": jsonEncode})
	// The documented script template calls require("json"), so provide exactly
	// that one module and nothing else.
	L.SetGlobal("require", L.NewFunction(func(L *lua.LState) int {
		if name := L.CheckString(1); name != "json" {
			L.RaiseError("module %q is not available", name)
		}
		L.Push(json)
		return 1
	}))
}
