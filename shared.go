package tsf

import (
	"context"
	"net/url"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/hisonsoft/tsf-go/gin"
	"github.com/hisonsoft/tsf-go/util"
)

func ServerOperation(ctx context.Context) (method string, operation string) {
	method = "POST"
	if c, ok := gin.FromGinContext(ctx); ok {
		operation = c.Ctx.FullPath()
		method = c.Ctx.Request.Method
	} else if tr, ok := transport.FromServerContext(ctx); ok {
		operation = tr.Operation()
		if tr.Kind() == transport.KindHTTP {
			if ht, ok := tr.(*http.Transport); ok {
				operation = ht.PathTemplate()
				method = ht.Request().Method
			}
		}
	}
	return
}

func ClientOperation(ctx context.Context) (method string, operation string) {
	method = "POST"
	if tr, ok := transport.FromClientContext(ctx); ok {
		operation = tr.Operation()
		if tr.Kind() == transport.KindHTTP {
			if ht, ok := tr.(*http.Transport); ok {
				operation = ht.PathTemplate()
				method = ht.Request().Method
			}
		}
	}
	return
}

func LocalEndpoint(ctx context.Context) (local struct {
	Service string
	IP      string
	Port    uint16
}) {
	k, _ := kratos.FromContext(ctx)
	if k != nil {
		local.Service = k.Name()
	}
	var localAddr string
	if tr, ok := transport.FromServerContext(ctx); ok {
		u, _ := url.Parse(tr.Endpoint())
		localAddr = u.Host
	}
	local.IP, local.Port = util.ParseAddr(localAddr)
	return
}
