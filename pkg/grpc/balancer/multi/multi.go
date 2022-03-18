package multi

import (
	"context"
	"sync"

	"github.com/hisonsoft/tsf-go/log"
	tBalancer "github.com/hisonsoft/tsf-go/pkg/balancer"
	"github.com/hisonsoft/tsf-go/pkg/balancer/p2c"
	"github.com/hisonsoft/tsf-go/pkg/balancer/p2ce"
	"github.com/hisonsoft/tsf-go/pkg/balancer/random"
	"github.com/hisonsoft/tsf-go/pkg/meta"
	"github.com/hisonsoft/tsf-go/pkg/naming"
	"github.com/hisonsoft/tsf-go/pkg/route"
	"github.com/openzipkin/zipkin-go"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
)

var (
	_ base.PickerBuilder = &Builder{}
	_ balancer.Picker    = &Picker{}

	mu sync.Mutex

	balancers []tBalancer.Balancer
)

func init() {
	// p2c
	b := p2c.Builder{}
	balancers = append(balancers, b.Build(context.Background(), nil, nil))

	// random
	balancers = append(balancers, &random.Picker{})

	b2 := p2ce.Builder{}
	balancers = append(balancers, b2.Build(context.Background(), nil, nil))
}

// Register register balancer builder if nil.
func Register(router route.Router) {
	mu.Lock()
	defer mu.Unlock()
	for _, b := range balancers {
		if balancer.Get(b.Schema()) == nil {
			balancer.Register(newBuilder(router, b))
		}
	}

}

// Set overwrite any balancer builder.
func Set(router route.Router) {
	mu.Lock()
	defer mu.Unlock()
	for _, b := range balancers {
		balancer.Register(newBuilder(router, b))
	}
}

type Builder struct {
	router route.Router
	b      tBalancer.Balancer
}

// newBuilder creates a new weighted-roundrobin balancer builder.
func newBuilder(router route.Router, b tBalancer.Balancer) balancer.Builder {
	return base.NewBalancerBuilder(
		b.Schema(),
		&Builder{router: router, b: b},
		base.Config{HealthCheck: true},
	)
}

func (b *Builder) Build(info base.PickerBuildInfo) balancer.Picker {
	p := &Picker{
		subConns: make(map[string]balancer.SubConn),
		r:        b.router,
		b:        b.b,
	}
	for conn, info := range info.ReadySCs {
		ins := info.Address.Attributes.Value("rawInstance").(naming.Instance)
		p.instances = append(p.instances, ins)
		p.subConns[ins.Addr()] = conn
	}
	return p
}

type Picker struct {
	instances []naming.Instance
	subConns  map[string]balancer.SubConn
	r         route.Router //路由&泳道
	b         tBalancer.Balancer
}

// Pick pick instances
func (p *Picker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	svc := naming.NewService(
		meta.Sys(info.Ctx, meta.DestKey(meta.ServiceNamespace)).(string),
		meta.Sys(info.Ctx, meta.DestKey(meta.ServiceName)).(string),
	)
	log.DefaultLog.WithContext(info.Ctx).Debugw("msg", "picker pick", "svc", svc, "nodes", p.instances)

	nodes := p.r.Select(info.Ctx, svc, p.instances)
	if len(nodes) == 0 {
		log.DefaultLog.WithContext(info.Ctx).Errorw("msg", "picker: ErrNoSubConnAvailable!", "service", svc.Name)
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	}
	node, _ := p.b.Pick(info.Ctx, nodes)
	span := zipkin.SpanFromContext(info.Ctx)
	if span != nil {
		ep, _ := zipkin.NewEndpoint(node.Service.Name, node.Addr())
		span.SetRemoteEndpoint(ep)
	}
	return balancer.PickResult{
		SubConn: p.subConns[node.Addr()],
		Done:    nil,
	}, nil
}
