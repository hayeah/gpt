//go:build wireinject

package gpt

import "github.com/google/wire"

func InitApp() (*App, error) {
	panic(wire.Build(wires))
}
