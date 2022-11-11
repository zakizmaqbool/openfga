package plugin

import (
	"io/ioutil"
	"path/filepath"
	"plugin"
	"strings"

	"github.com/openfga/openfga/pkg/authorizer"
)

func LoadPlugins(path string) (*PluginManager, error) {
	c, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	pm := &PluginManager{}

	for _, entry := range c {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".so") {
			fullpath := filepath.Join(path, entry.Name())

			p, err := plugin.Open(fullpath)
			if err != nil {
				return nil, err
			}

			ifunc, err := p.Lookup("InitPlugin")
			if err != nil {
				return nil, err
			}

			initFunc := ifunc.(func(*PluginManager) error)
			if err := initFunc(pm); err != nil {
				return nil, err
			}
		}
	}

	return pm, nil
}

type PluginManager struct {
	authorizerMiddleware authorizer.AuthorizerMiddleware
}

func (p *PluginManager) RegisterAuthorizerMiddleware(fn authorizer.AuthorizerMiddleware) {
	p.authorizerMiddleware = fn
}

func (p *PluginManager) AuthorizerMiddleware() authorizer.AuthorizerMiddleware {
	return p.authorizerMiddleware
}
