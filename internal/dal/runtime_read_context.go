package dal

import "context"

// ReadContext preserves legacy callers while scoped runtime reads explicitly
// carry the request deadline through every SQL and external-store operation.
func ReadContext(contexts ...context.Context) context.Context {
	if len(contexts) > 0 && contexts[0] != nil {
		return contexts[0]
	}
	return context.Background()
}
