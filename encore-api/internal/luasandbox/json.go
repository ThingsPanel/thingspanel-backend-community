package luasandbox

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"

	lua "github.com/yuin/gopher-lua"
)

const maxJSONDepth = 64

func jsonDecode(L *lua.LState) int {
	var decoded any
	if err := json.Unmarshal([]byte(L.CheckString(1)), &decoded); err != nil {
		L.RaiseError("json.decode: %s", err.Error())
	}
	L.Push(toLua(L, decoded, 0))
	return 1
}

func jsonEncode(L *lua.LState) int {
	value, err := fromLua(L.CheckAny(1), 0, map[*lua.LTable]bool{})
	if err != nil {
		L.RaiseError("json.encode: %s", err.Error())
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		L.RaiseError("json.encode: %s", err.Error())
	}
	L.Push(lua.LString(encoded))
	return 1
}

func toLua(L *lua.LState, v any, depth int) lua.LValue {
	if depth > maxJSONDepth {
		L.RaiseError("json.decode: nesting is too deep")
	}
	switch value := v.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(value)
	case float64:
		return lua.LNumber(value)
	case string:
		return lua.LString(value)
	case []any:
		table := L.CreateTable(len(value), 0)
		for _, item := range value {
			table.Append(toLua(L, item, depth+1))
		}
		return table
	case map[string]any:
		table := L.CreateTable(0, len(value))
		for key, item := range value {
			table.RawSetString(key, toLua(L, item, depth+1))
		}
		return table
	}
	return lua.LNil
}

type encodeError string

func (e encodeError) Error() string { return string(e) }

func fromLua(v lua.LValue, depth int, seen map[*lua.LTable]bool) (any, error) {
	if depth > maxJSONDepth {
		return nil, encodeError("nesting is too deep")
	}
	switch value := v.(type) {
	case *lua.LNilType:
		return nil, nil
	case lua.LBool:
		return bool(value), nil
	case lua.LNumber:
		number := float64(value)
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, encodeError("numbers must be finite")
		}
		return number, nil
	case lua.LString:
		return string(value), nil
	case *lua.LTable:
		if seen[value] {
			return nil, encodeError("circular reference")
		}
		seen[value] = true
		defer delete(seen, value)
		if length := value.Len(); length > 0 && isArray(value, length) {
			items := make([]any, 0, length)
			for i := 1; i <= length; i++ {
				item, err := fromLua(value.RawGetInt(i), depth+1, seen)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return items, nil
		}
		object := map[string]any{}
		var failure error
		keys := []string{}
		values := map[string]lua.LValue{}
		value.ForEach(func(key, item lua.LValue) {
			name, ok := key.(lua.LString)
			if !ok {
				if number, isNumber := key.(lua.LNumber); isNumber {
					name, ok = lua.LString(strconv.FormatFloat(float64(number), 'f', -1, 64)), true
				}
			}
			if !ok {
				failure = encodeError("object keys must be strings")
				return
			}
			keys = append(keys, string(name))
			values[string(name)] = item
		})
		if failure != nil {
			return nil, failure
		}
		sort.Strings(keys)
		for _, key := range keys {
			item, err := fromLua(values[key], depth+1, seen)
			if err != nil {
				return nil, err
			}
			object[key] = item
		}
		return object, nil
	}
	return nil, encodeError("unsupported value of type " + v.Type().String())
}

func isArray(table *lua.LTable, length int) bool {
	count := 0
	table.ForEach(func(lua.LValue, lua.LValue) { count++ })
	return count == length
}
