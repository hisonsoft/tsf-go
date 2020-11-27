package server

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tencentyun/tsf-go/pkg/auth"
	"github.com/tencentyun/tsf-go/pkg/auth/authenticator"
	cfgConsul "github.com/tencentyun/tsf-go/pkg/config/consul"
	tgrpc "github.com/tencentyun/tsf-go/pkg/grpc"         // NOTE: open json encoding by set header Content-Type: application/grpc+json
	"github.com/tencentyun/tsf-go/pkg/grpc/encoding/json" // NOTE: open json encoding by set header Content-Type: application/grpc+json
	"github.com/tencentyun/tsf-go/pkg/log"
	"github.com/tencentyun/tsf-go/pkg/naming"
	"github.com/tencentyun/tsf-go/pkg/naming/consul"
	"github.com/tencentyun/tsf-go/pkg/proxy"
	"github.com/tencentyun/tsf-go/pkg/sys/apiMeta"
	"github.com/tencentyun/tsf-go/pkg/sys/env"
	"github.com/tencentyun/tsf-go/pkg/sys/trace"
	"github.com/tencentyun/tsf-go/pkg/util"

	"github.com/openzipkin/zipkin-go"
	zipkingrpc "github.com/openzipkin/zipkin-go/middleware/grpc"
	"go.uber.org/zap"
	grpc "google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // NOTE: use grpc gzip by header grpc-accept-encoding
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type Config struct {
	// 服务名称，命名空间内唯一的调用标识
	ServerName string
	// 服务监听的端口
	Port int
}

// Server is the framework's server side instance, it contains the GrpcServer, interceptor and interceptors.
// Create an instance of Server, by using NewServer().
type Server struct {
	*grpc.Server
	conf   *Config
	authen auth.Auth
	tracer *zipkin.Tracer

	interceptors []grpc.UnaryServerInterceptor
}

// NewServer create a grpc server instance
func NewServer(conf *Config, o ...grpc.ServerOption) (s *Server) {
	var (
		opts []grpc.ServerOption
	)

	json.Init()
	util.ParseFlag()
	s = &Server{conf: s.fixConf(conf)}

	// create our local service endpoint
	endpoint, err := zipkin.NewEndpoint(s.conf.ServerName, fmt.Sprintf("%s:%d", env.LocalIP(), s.conf.Port))
	if err != nil {
		panic(err)
	}
	// initialize our tracer
	tracer, err := zipkin.NewTracer(trace.GetReporter(), zipkin.WithLocalEndpoint(endpoint))
	if err != nil {
		panic(err)
	}
	s.tracer = tracer

	// append system defined grpc options first
	opts = append(opts,
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     time.Second * 120,
			MaxConnectionAgeGrace: time.Second * 15,
			Time:                  time.Second * 30,
			Timeout:               time.Second * 10,
			// 防止max stream id 溢出的问题
			MaxConnectionAge: time.Hour * 4,
		}),
		grpc.UnaryInterceptor(s.chainUnaryInterceptors()),
		grpc.StatsHandler(zipkingrpc.NewServerHandler(tracer)),
	)

	// can be overwritten by user defined grpc options except UnaryInterceptor(which will cause panic)
	opts = append(opts, o...)
	s.Server = grpc.NewServer(opts...)
	builder := &authenticator.Builder{}
	s.authen = builder.Build(cfgConsul.DefaultConsul(), naming.NewService(env.NamespaceID(), conf.ServerName))
	s.Use(s.handle)
	return
}

func (s *Server) fixConf(conf *Config) *Config {
	var newConf Config
	if conf != nil {
		newConf = *conf
	}
	if conf.Port == 0 {
		if env.Port() != 0 {
			newConf.Port = env.Port()
		} else {
			newConf.Port = 8080
		}
	}
	if conf.ServerName == "" {
		newConf.ServerName = env.ServiceName()
	}
	return &newConf
}

// Use attachs a global inteceptor to the server.
// For example, this is the right place for a rate limiter or error management inteceptor.
// This function is not concurrency safe.
func (s *Server) Use(interceptors ...grpc.UnaryServerInterceptor) *Server {
	s.interceptors = append(s.interceptors, interceptors...)
	return s
}

// Start create a tcp listener and start goroutine for serving each incoming request.
// Start will block until term signal is received.
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", int64(s.conf.Port)))
	if err != nil {
		return err
	}
	reflection.Register(s.Server)

	go func() {
		if env.DisableGrpcHttp() {
			log.L().Info(context.Background(), "grpc server start serve and listen!", zap.String("name", s.conf.ServerName), zap.Int("port", s.conf.Port))
			err = s.Serve(lis)
			if err != nil {
				panic(err)
			}
		} else {
			log.L().Info(context.Background(), "grpc&http server start serve and listen!", zap.String("name", s.conf.ServerName), zap.Int("port", s.conf.Port))
			serveHttp(s.Server, lis)
			/*err = s.Serve(lis)
			if err != nil {
				panic(err)
			}*/
		}
	}()
	ip := env.LocalIP()
	port := s.conf.Port
	serDesc, err := tgrpc.GetServiceMethods(fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		log.L().Errorf(context.Background(), "GetServiceMethods failed", zap.String("addr", fmt.Sprintf("%s:%d", ip, port)), zap.Error(err))
	}
	api := apiMeta.GenApiMeta(serDesc)
	var apiStr string
	if len(api.Paths) > 0 {
		apiStr, err = apiMeta.Encode(api)
		if err != nil {
			log.L().Error(context.Background(), "[grpc server] encode api failed!", zap.Any("api", api), zap.Error(err))
		}
	}
	if proxy.Inited() && env.RemoteIP() != "" {
		ip = env.RemoteIP()
		port = int(rand.Int31n(55535)) + 10000
		proxy.ListenRemote(s.conf.Port, int(port))
	}
	svc := naming.NewService(env.NamespaceID(), s.conf.ServerName)
	ins := naming.Instance{
		ID:      env.InstanceId(),
		Service: &svc,
		Host:    ip,
		Port:    port,
		Metadata: map[string]string{
			"TSF_APPLICATION_ID": env.ApplicationID(),
			"TSF_GROUP_ID":       env.GroupID(),
			"TSF_INSTNACE_ID":    env.InstanceId(),
			"TSF_PROG_VERSION":   env.ProgVersion(),
			"TSF_ZONE":           env.Zone(),
			"TSF_REGION":         env.Region(),
			"protocol":           "grpc",
			"TSF_API_METAS":      apiStr,
			"TSF_NAMESPACE_ID":   env.NamespaceID(),
		},
	}
	err = consul.DefaultConsul().Register(&ins)
	if err != nil {
		time.Sleep(time.Millisecond * 500)
		err = consul.DefaultConsul().Register(&ins)
	}
	if err != nil {
		return err
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL, syscall.SIGHUP)
	sig := <-sigs
	log.L().Info(context.Background(), "[server] got signal,exit now!", zap.String("sig", sig.String()), zap.String("name", s.conf.ServerName))
	consul.DefaultConsul().Deregister(&ins)
	time.Sleep(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	go func() {
		s.GracefulStop()
		trace.GetReporter().Close()
		cancel()
	}()
	<-ctx.Done()
	if errors.Is(context.DeadlineExceeded, ctx.Err()) {
		log.L().Error(ctx, "[server] graceful shutdown failed!", zap.String("name", s.conf.ServerName))
		s.Stop()
	} else {
		log.L().Info(ctx, "[server] graceful shutdown success!", zap.String("name", s.conf.ServerName))
	}
	return nil
}

// chainUnaryInterceptors creates a single interceptor out of a chain of many interceptors.
// Execution is done in left-to-right order, including passing of context.
// For example ChainUnaryServer(one, two, three) will execute one before two before three, and three
// will see context changes of one and two.
func (s *Server) chainUnaryInterceptors() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		n := len(s.interceptors)
		chainer := func(currentInter grpc.UnaryServerInterceptor, currentHandler grpc.UnaryHandler) grpc.UnaryHandler {
			return func(currentCtx context.Context, currentReq interface{}) (interface{}, error) {
				return currentInter(currentCtx, currentReq, info, currentHandler)
			}
		}

		chainedHandler := handler
		for i := n - 1; i >= 0; i-- {
			chainedHandler = chainer(s.interceptors[i], chainedHandler)
		}

		return chainedHandler(ctx, req)
	}
}