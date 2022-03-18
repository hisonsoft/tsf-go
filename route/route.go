package route

import (
	"context"

	"github.com/hisonsoft/tsf-go/naming"
)

type Router interface {
	Select(ctx context.Context, svc naming.Service, nodes []naming.Instance) (selects []naming.Instance)
}
