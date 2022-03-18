package random

import (
	"context"
	"math/rand"

	"github.com/hisonsoft/tsf-go/balancer"
	"github.com/hisonsoft/tsf-go/naming"
)

var (
	_ balancer.Balancer = &Picker{}

	Name = "random"
)

type Picker struct {
}

func New() *Picker {
	return &Picker{}
}

func (p *Picker) Pick(ctx context.Context, nodes []naming.Instance) (node *naming.Instance, done func(balancer.DoneInfo)) {
	if len(nodes) == 0 {
		return nil, func(balancer.DoneInfo) {}
	}
	cur := rand.Intn(len(nodes))
	return &nodes[cur], func(balancer.DoneInfo) {}
}

func (p *Picker) Schema() string {
	return Name
}
