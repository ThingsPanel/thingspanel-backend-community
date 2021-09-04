package di

import "github.com/ThingsPanel/ThingsPanel-Go/pkg/config"

// Package di is a simple container implementation in singleton pattern to avoid
// duplicate instantiation of objects. Also acts as a storage for application
// configurations.

// Container : Defines the container interface
type Container interface {
	// BindModuleConfig : Binds a static configuration to container.
	BindModuleConfig(config config.Config)
	// ResolveModuleConfig : Resolves a static configuration by name. Returns error if not found.
	ResolveModuleConfig(name string) (c config.Config, err error)
	// Bind : Binds an instance of a module/implementation to the container
	Bind(name string, value interface{})
	// Resolve : Resolves an instance of a module/implementation by name
	Resolve(name string) (value interface{}, err error)
	// Init : Initializes the modules in the order provided.
	Init(modules ...string) error
	// Start : Start the modules in the order provided. Only runnable modules should be provided here.
	Start(modules ...string) error
	// Stop : Stop the modules in the order provided. Only runnable modules should be provided here.
	Stop(modules ...string) error
}

// Module : Defines the singleton instance of a modular unit, with a specific application functionality.
type Module interface {
	// Name : Returns name of the module as a string. Must be a unique reference.
	Name() string
	// Init : Initialize the module, module bindings should be resolved within this function.
	Init(c Container) error
}

// Runnable : Should be implemented if the module has a runnable process.
type Runnable interface {
	// Start : starts the runnable process
	Start() error
	// Stop : stops the runnable process
	Stop() error
}
