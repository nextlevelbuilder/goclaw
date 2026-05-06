package tool

import (
	"fmt"
	"log"
	"sync"
)

// ToolFactory is a function that creates a Tool instance.
// Tool factories enable lazy initialization and on-demand tool creation.
//
// Example:
//
//	func init() {
//	    tool.GlobalToolFactoryRegistry.Register("my_tool", func() tool.Tool {
//	        return &MyTool{}
//	    })
//	}
type ToolFactory func() Tool

// ToolFactoryRegistry manages tool factories for external registration.
// External packages can register tool factories, and goClaw will automatically
// create and register tool instances at startup.
type ToolFactoryRegistry struct {
	mu        sync.RWMutex
	factories map[string]ToolFactory
}

// NewToolFactoryRegistry creates a new tool factory registry.
func NewToolFactoryRegistry() *ToolFactoryRegistry {
	return &ToolFactoryRegistry{
		factories: make(map[string]ToolFactory),
	}
}

// Register registers a tool factory with the given name.
// Panics if a factory with the same name is already registered.
//
// This method is thread-safe and can be called from multiple goroutines
// (typically from init() functions in different packages).
func (r *ToolFactoryRegistry) Register(name string, factory ToolFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		panic(fmt.Sprintf("Tool factory already registered: %s", name))
	}

	r.factories[name] = factory
	log.Printf("[ToolFactory] Registered tool factory: %s", name)
}

// Create creates a new tool instance from the registered factory.
// Returns the tool instance and true if the factory exists,
// or nil and false if no factory is registered with the given name.
//
// Each call to Create returns a new tool instance (lazy initialization).
func (r *ToolFactoryRegistry) Create(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, exists := r.factories[name]
	if !exists {
		return nil, false
	}

	return factory(), true
}

// List returns all registered factory names.
// The returned slice is sorted alphabetically.
func (r *ToolFactoryRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// Count returns the number of registered tool factories.
func (r *ToolFactoryRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.factories)
}

// GlobalToolFactoryRegistry is the global registry for external tool factories.
// External packages should register their factories in init() functions.
//
// Example:
//
//	import "github.com/nextlevelbuilder/goclaw/pkg/tool"
//
//	func init() {
//	    tool.GlobalToolFactoryRegistry.Register("my_tool", func() tool.Tool {
//	        return &MyTool{}
//	    })
//	}
//
// goClaw will automatically create and register tools from these factories
// during startup, before the agent loop begins.
var GlobalToolFactoryRegistry = NewToolFactoryRegistry()
