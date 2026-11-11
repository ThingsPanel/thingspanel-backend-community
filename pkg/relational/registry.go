package relational

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"
)

// Registry holds the handles of one process: exactly one per logical resource. It is how "one pool per resource, and
// whoever created it closes it" is enforced: a second Register of the same name is an error, and Close closes only the
// pools the process opened itself (owned), never one it merely adopted.
type Registry struct {
	mu      sync.Mutex
	entries map[string]entry
}

type entry struct {
	db    *gorm.DB
	owned bool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{entries: map[string]entry{}} }

// Register stores db under name. owned=true for a handle from Open; owned=false for one from Adopt.
func (r *Registry) Register(name string, db *gorm.DB, owned bool) error {
	if name == "" || db == nil {
		return errors.New("relational: register needs a name and a handle")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.entries[name]; dup {
		return fmt.Errorf("relational: resource %q already has a connection pool", name)
	}
	r.entries[name] = entry{db: db, owned: owned}
	return nil
}

// Get returns the handle registered under name.
func (r *Registry) Get(name string) (*gorm.DB, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[name]
	return e.db, ok
}

// Names lists the registered resources, sorted.
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.entries))
	for n := range r.entries {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Close closes every owned pool and forgets all entries. It returns the joined errors.
func (r *Registry) Close() error {
	r.mu.Lock()
	entries := r.entries
	r.entries = map[string]entry{}
	r.mu.Unlock()
	var errs []error
	for name, e := range entries {
		if !e.owned {
			continue
		}
		if err := CloseDB(e.db); err != nil {
			errs = append(errs, fmt.Errorf("close %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// CloseOne forgets name and closes its pool when the registry owns it. Closing a name that is not registered is not
// an error (a service may shut down twice).
func (r *Registry) CloseOne(name string) error {
	r.mu.Lock()
	e, ok := r.entries[name]
	delete(r.entries, name)
	r.mu.Unlock()
	if !ok || !e.owned {
		return nil
	}
	return CloseDB(e.db)
}
