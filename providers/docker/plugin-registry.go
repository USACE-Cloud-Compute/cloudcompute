package cloudcompute

import (
	"fmt"

	. "github.com/usace-cloud-compute/cloudcompute"
)

type PluginRegistry interface {
	Register(plugin *Plugin) error
	Deregister(name string) error
	Get(name string) (*Plugin, error)
}

func NewInMemoryPluginRegistry() PluginRegistry {
	return &InMemoryPluginRegistry{
		plugins: make(map[string]*Plugin),
	}
}

type InMemoryPluginRegistry struct {
	plugins map[string]*Plugin
}

func (pr *InMemoryPluginRegistry) Register(plugin *Plugin) error {
	pr.plugins[plugin.Name] = plugin
	return nil
}

func (pr *InMemoryPluginRegistry) Deregister(name string) error {
	delete(pr.plugins, name)
	return nil
}

func (pr *InMemoryPluginRegistry) Get(name string) (*Plugin, error) {
	plugin, ok := pr.plugins[name]
	if !ok {
		return nil, fmt.Errorf("Plugin %s is not registered", name)
	}
	return plugin, nil
}
