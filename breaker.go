package tsf

import (
	"context"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/hisonsoft/tsf-go/breaker"
)

func BreakerMiddleware(opts ...ClientOption) middleware.Middleware {
	var o clientOpionts
	for _, opt := range opts {
		opt(&o)
	}
	if o.breakerCfg != nil && o.breakerCfg.SwitchOff {
		return func(handler middleware.Handler) middleware.Handler {
			return func(ctx context.Context, req interface{}) (interface{}, error) {
				return handler(ctx, req)
			}
		}
	}
	group := breaker.NewGroup(o.breakerCfg)
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (reply interface{}, err error) {
			if tr, ok := transport.FromClientContext(ctx); ok {
				if tr.Operation() != "" {
					breaker := group.Get(tr.Operation())
					if err = breaker.Allow(); err != nil {
						return
					}
					defer func() {
						if err != nil {
							if o.breakerErrorHook != nil {
								if !o.breakerErrorHook(ctx, tr.Operation(), err) {
									breaker.MarkFailed()
									return
								}
							} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.FromError(err).GetCode() >= 500 {
								breaker.MarkFailed()
								return
							}
						}
						breaker.MarkSuccess()
					}()
				}
			}
			reply, err = handler(ctx, req)
			return
		}
	}
}
