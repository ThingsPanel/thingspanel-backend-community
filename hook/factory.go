package hook

import "fmt"

type HookFactory func() Hook

var HookFactoryMap = make(map[string]HookFactory)

func RegisterHookFactory(name string, factory HookFactory) {
	HookFactoryMap[name] = factory
}

func CreateHook(name string) (Hook, error) {
	if factory, ok := HookFactoryMap[name]; ok {
		return factory(), nil
	} else {
		return nil, fmt.Errorf("HookFactory %s not found", name)
	}
}
