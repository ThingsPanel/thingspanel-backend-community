package di

import (
	"github.com/ThingsPanel/ThingsPanel-Go/pkg/config"
	"github.com/ThingsPanel/ThingsPanel-Go/pkg/errors"
)

type container struct {
	bindings map[string]interface{}
	configs  map[string]config.Config
}

func NewContainer() *container {
	return &container{
		bindings: make(map[string]interface{}),
		configs:  make(map[string]config.Config),
	}
}

func (c *container) Start(modules ...string) (err error) {
	for index := range modules {
		m := modules[index]

		b, ok := c.bindings[m]
		if !ok {
			return errors.Errorf(`provided binding: [%s] not found, use container.Bind() to bind first`,
				modules[index])
		}

		r, ok := b.(Runnable)
		if !ok {
			return errors.Errorf(`provided binding: [%s] does not implement di.Runnable`, m)
		}

		err = r.Start()
		if err != nil {
			return errors.Errorf(`failed to start module: [%s] due: [%s]`, m, err)
		}
	}

	return nil
}

func (c *container) Stop(modules ...string) (err error) {
	for index := range modules {
		m := modules[index]

		b, ok := c.bindings[m]
		if !ok {
			return errors.Errorf(`provided binding: [%s] not found, use container.Bind() to bind first`,
				modules[index])
		}

		r, ok := b.(Runnable)
		if !ok {
			return errors.Errorf(`provided binding: [%s] does not implement di.Runnable`, m)
		}

		err = r.Stop()
		if err != nil {
			return errors.Errorf(`failed to stop module: [%s] due: [%s]`, m, err)
		}
	}

	return nil
}

func (c *container) BindModuleConfig(name string, config config.Config) {
	c.configs[name] = config
}

func (c *container) ResolveModuleConfig(name string) (config.Config, error) {
	conf, ok := c.configs[name]
	if !ok {
		return conf, errors.Errorf(`no config for name: %s found, use container.Bind() first`, name)
	}

	return conf, nil
}

func (c *container) Bind(name string, value interface{}) {
	c.bindings[name] = value
}

func (c *container) Resolve(name string) (value interface{}, err error) {
	val, ok := c.bindings[name]
	if !ok {
		return value, errors.Errorf(`failed to resolve binding for name: %s`, name)
	}

	return val, nil
}

func (c *container) Init(modules ...string) (err error) {
	for index := range modules {
		name := modules[index]

		b, ok := c.bindings[name]
		if !ok {
			return errors.Errorf(`provided binding: [%s] not found, use container.Bind() first`, name)
		}

		m, ok := b.(Module)
		if !ok {
			return errors.Errorf(`provided binding: [%s] does not implement di.Module interface`, name)
		}
		err = m.Init(c)
		if err != nil {
			return errors.Errorf(`failed to init module: [%s] due: [%s]`, m.Name(), err)
		}
	}

	return nil
}
