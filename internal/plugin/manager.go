package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

type lookupFunc func(name string) (pluginapi.FactoryWithStores, bool)

type Manager struct {
	plugins map[string]pluginapi.Store
	dedup   map[string]bool
	// order lists instances in build order, so an inner instance comes before
	// every instance that uses it.
	order []string

	// Set only while LoadPlugins runs.
	defs  map[string]pluginapi.PluginDefinition
	stack []string

	lookup lookupFunc
}

func NewManager() *Manager {
	return newManagerWithLookup(pluginapi.LookupFactory)
}

func newManagerWithLookup(lookup lookupFunc) *Manager {
	return &Manager{
		plugins: make(map[string]pluginapi.Store),
		dedup:   make(map[string]bool),
		lookup:  lookup,
	}
}

// LoadPlugins builds every defined instance, building an instance's inner
// instances first when its factory asks for them through PluginStores. Names
// already built are reused; a changed definition for an existing name is
// ignored. On error, every instance built by this call is closed and dropped.
func (m *Manager) LoadPlugins(ctx context.Context, definitions map[string]pluginapi.PluginDefinition) error {
	start := len(m.order)
	m.defs = definitions
	defer func() {
		m.defs = nil
		m.stack = nil
	}()

	for _, name := range slices.Sorted(maps.Keys(definitions)) {
		if _, err := m.get(ctx, name); err != nil {
			return errors.Join(err, m.rollback(start))
		}
	}
	return nil
}

// get returns the named instance, building it first if needed.
func (m *Manager) get(ctx context.Context, name string) (pluginapi.Store, error) {
	if store, ok := m.plugins[name]; ok {
		return store, nil
	}
	if slices.Contains(m.stack, name) {
		return nil, fmt.Errorf("plugin cycle: %s", strings.Join(append(slices.Clone(m.stack), name), " -> "))
	}
	def, ok := m.defs[name]
	if !ok {
		if len(m.stack) == 0 {
			return nil, fmt.Errorf("plugin %q is not defined", name)
		}
		return nil, fmt.Errorf("plugin %q referenced by %q is not defined", name, m.stack[len(m.stack)-1])
	}

	m.stack = append(m.stack, name)
	stores := &managerStores{ctx: ctx, m: m, owner: name}
	store, err := m.createPlugin(ctx, def, stores)
	stores.done = true
	m.stack = m.stack[:len(m.stack)-1]
	if err != nil {
		return nil, fmt.Errorf("failed to create plugin %s: %w", name, err)
	}
	if store == nil {
		return nil, fmt.Errorf("plugin %q: factory returned no store", name)
	}

	m.plugins[name] = store
	m.dedup[name] = parseDedup(def.Config["dedup"])
	m.order = append(m.order, name)
	return store, nil
}

// rollback closes and drops the instances built since order[start].
func (m *Manager) rollback(start int) error {
	err := m.closeAll(m.order[start:])
	m.order = m.order[:start]
	return err
}

func (m *Manager) GetPlugin(name string) (pluginapi.Store, error) {
	store, ok := m.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %s not found", name)
	}
	return store, nil
}

// Close closes every plugin instance that implements io.Closer, in reverse
// build order so an instance closes before the inner instances it uses, and
// joins any errors. It is idempotent: a second call is a no-op returning nil.
func (m *Manager) Close() error {
	err := m.closeAll(m.order)
	m.order = nil
	return err
}

// closeAll closes and drops names in reverse order.
func (m *Manager) closeAll(names []string) error {
	var errs []error
	for _, name := range slices.Backward(names) {
		if closer, ok := m.plugins[name].(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("plugin %s: %w", name, err))
			}
		}
		delete(m.plugins, name)
		delete(m.dedup, name)
	}
	return errors.Join(errs...)
}

// IsDedup reports whether the named plugin instance has dedup enabled.
// Unknown plugin names return false.
func (m *Manager) IsDedup(name string) bool {
	return m.dedup[name]
}

// parseDedup coerces a raw config value into a dedup flag. It accepts a
// real bool, or a string such as "true"/"1" (e.g. from an environment
// override). Anything else, including an absent value (nil), is false.
func parseDedup(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

// managerStores is the PluginStores handed to the factory building owner.
type managerStores struct {
	ctx   context.Context
	m     *Manager
	owner string
	done  bool
}

func (d *managerStores) Lookup(name string) (pluginapi.Store, error) {
	if d.done {
		return nil, fmt.Errorf("plugin %q: PluginStores used after construction", d.owner)
	}
	return d.m.get(d.ctx, name)
}

func (m *Manager) createPlugin(ctx context.Context, def pluginapi.PluginDefinition, stores pluginapi.PluginStores) (pluginapi.Store, error) {
	switch def.Type {
	case "exec":
		return NewExecPlugin(def.Config)
	case "shell":
		return NewShellPlugin(def.Config)
	case "builtin":
		return newBuiltinPlugin(ctx, def.Config, stores, m.lookup)
	default:
		return nil, fmt.Errorf("unsupported plugin type: %s", def.Type)
	}
}
