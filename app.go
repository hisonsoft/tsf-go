package tsf

import (
	"github.com/hisonsoft/tsf-go/naming/consul"
	"github.com/hisonsoft/tsf-go/pkg/sys/env"
	"github.com/hisonsoft/tsf-go/pkg/version"

	"github.com/go-kratos/kratos/v2"
	"github.com/hisonsoft/swagger-api/openapiv2"
	"google.golang.org/grpc"
)

// Option is HTTP server option.
type Option func(*appOptions)

// ProtoServiceName specific the proto service name(<package_name.service_name>)
// generated by swagger api
func ProtoServiceName(fullname string) Option {
	return func(a *appOptions) {
		a.protoService = fullname
	}
}

func GRPCServer(srv *grpc.Server) Option {
	return func(a *appOptions) {
		a.srv = srv
	}
}

func EnableReigstry(enable bool) Option {
	return func(a *appOptions) {
		a.enableReigstry = enable
	}
}

func Medata(metadata map[string]string) Option {
	return func(a *appOptions) {
		a.metadata = metadata
	}
}

type appOptions struct {
	protoService   string
	srv            *grpc.Server
	apiMeta        bool
	enableReigstry bool
	metadata       map[string]string
}

func APIMeta(enable bool) Option {
	return func(a *appOptions) {
		a.apiMeta = enable
	}
}

func Metadata(optFuncs ...Option) (opt kratos.Option) {
	enableApiMeta := true
	if env.Token() == "" {
		enableApiMeta = false
	}

	var opts appOptions = appOptions{}
	for _, o := range optFuncs {
		o(&opts)
	}
	if opts.apiMeta {
		enableApiMeta = true
	}

	md := map[string]string{
		"TSF_APPLICATION_ID": env.ApplicationID(),
		"TSF_GROUP_ID":       env.GroupID(),
		"TSF_INSTNACE_ID":    env.InstanceId(),
		"TSF_PROG_VERSION":   env.ProgVersion(),
		"TSF_ZONE":           env.Zone(),
		"TSF_REGION":         env.Region(),
		"TSF_NAMESPACE_ID":   env.NamespaceID(),
		"TSF_SDK_VERSION":    version.GetHumanVersion(),
	}
	if len(opts.metadata) > 0 {
		for k, v := range opts.metadata {
			md[k] = v
		}
	}
	if enableApiMeta {
		apiSrv := openapiv2.New(opts.srv)
		genAPIMeta(md, apiSrv, opts.protoService)
	}

	for k, v := range md {
		if v == "" || k == "" {
			delete(md, k)
		}
	}

	opt = kratos.Metadata(md)
	return
}

func ID(optFuncs ...Option) kratos.Option {
	return kratos.ID(env.InstanceId())
}
func Registrar(optFuncs ...Option) kratos.Option {
	return kratos.Registrar(consul.DefaultConsul())
}

func AppOptions(opts ...Option) []kratos.Option {
	o := appOptions{enableReigstry: true}
	for _, opt := range opts {
		opt(&o)
	}

	kopts := []kratos.Option{
		ID(opts...), Metadata(opts...),
	}
	if o.enableReigstry {
		kopts = append(kopts, Registrar(opts...))
	}
	return kopts
}
