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

	"github.com/hisonsoft/tsf-go/log"
	"github.com/hisonsoft/tsf-go/pkg/auth"
	"github.com/hisonsoft/tsf-go/pkg/auth/authenticator"
	cfgConsul "github.com/hisonsoft/tsf-go/pkg/config/consul"
	tgrpc "github.com/hisonsoft/tsf-go/pkg/grpc"         // NOTE: open json encoding by set header Content-Type: application/grpc+json
	"github.com/hisonsoft/tsf-go/pkg/grpc/encoding/json" // NOTE: open json encoding by set header Content-Type: application/grpc+json
	"github.com/hisonsoft/tsf-go/pkg/naming"
	"github.com/hisonsoft/tsf-go/pkg/naming/consul"
	"github.com/hisonsoft/tsf-go/pkg/proxy"
	"github.com/hisonsoft/tsf-go/pkg/sys/apiMeta"
	"github.com/hisonsoft/tsf-go/pkg/sys/env"
	"github.com/hisonsoft/tsf-go/pkg/sys/metrics"
	"github.com/hisonsoft/tsf-go/pkg/sys/trace"
	"github.com/hisonsoft/tsf-go/pkg/util"
	"github.com/hisonsoft/tsf-go/pkg/version"

	"github.com/openzipkin/zipkin-go"
	zipkingrpc "github.com/openzipkin/zipkin-go/middleware/grpc"
	"go.uber.org/zap"
	grpc "google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // NOTE: use grpc gzip by header grpc-accept-encoding
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// Config 是gRPC server的配置
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
	healthService *health.Server

	conf   *Config
	authen auth.Auth
	tracer *zipkin.Tracer

	interceptors       []grpc.UnaryServerInterceptor
	streamInterceptors []grpc.StreamServerInterceptor
	stopHook           func(ctx context.Context) error
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
		grpc.StreamInterceptor(s.chainStreamServer()),
		grpc.StatsHandler(zipkingrpc.NewServerHandler(tracer)),
	)
	// can be overwritten by user defined grpc options except UnaryInterceptor(which will cause panic)
	opts = append(opts, o...)
	s.Server = grpc.NewServer(opts...)
	builder := &authenticator.Builder{}
	s.authen = builder.Build(cfgConsul.DefaultConsul(), naming.NewService(env.NamespaceID(), conf.ServerName))
	s.Use(s.recovery, s.handle)
	s.UseStream(s.recoveryStream, s.handleStream)

	// register default health check service
	healthService := health.NewServer()
	grpc_health_v1.RegisterHealthServer(s.Server, healthService)
	s.healthService = healthService
	return
}

func (s *Server) fixConf(conf *Config) *Config {
	var newConf Config
	if conf != nil {
		newConf = *conf
	}
	if conf.Port == 0 {
		newConf.Port = env.Port()
	}
	if conf.ServerName == "" {
		newConf.ServerName = env.ServiceName()
	}
	return &newConf
}

// OnStop add stop hook to grpc server when server got terminating signal
// 默认传入一个10s的timeout的context
func (s *Server) OnStop(hook func(ctx context.Context) error) {
	s.stopHook = hook
}

// Start create a tcp listener and start goroutine for serving each incoming request.
// Start will block until term signal is received.
func (s *Server) Start() error {
	metrics.StartAgent()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", int64(s.conf.Port)))
	if err != nil {
		return err
	}
	reflection.Register(s.Server)

	go func() {
		if env.DisableGrpcHttp() {
			log.DefaultLog.Infow("msg", "grpc server start serve and listen!", "name", s.conf.ServerName, "port", s.conf.Port)
			err = s.Serve(lis)
			if err != nil {
				panic(err)
			}
		} else {
			log.DefaultLog.Infow("msg", "grpc&http server start serve and listen!", "name", s.conf.ServerName, "port", s.conf.Port)
			serveHttp(s.Server, lis)
		}
	}()

	ip := env.LocalIP()
	port := s.conf.Port
	serDesc, err := tgrpc.GetServiceMethods(fmt.Sprintf("%s:%d", ip, port))
	if err != nil {
		log.DefaultLog.Errorw("msg", "GetServiceMethods failed", "addr", fmt.Sprintf("%s:%d", ip, port), "err", zap.Error(err))
	}
	api := apiMeta.GenApiMeta(serDesc)
	var apiStr string
	if len(api.Paths) > 0 {
		apiStr, err = apiMeta.Encode(api)
		if err != nil {
			log.DefaultLog.Errorw("msg", "[grpc server] encode api failed!", "api", api, "err", err)
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
			"TSF_SDK_VERSION":    version.GetHumanVersion(),
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
	log.DefaultLog.Infow("msg", "[server] got signal,exit now!", "sig", sig.String(), "name", s.conf.ServerName)
	consul.DefaultConsul().Deregister(&ins)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	if s.stopHook != nil {
		err := s.stopHook(ctx)
		if err != nil {
			log.DefaultLog.Errorw("msg", "[server] stophook exec failed!", "name", s.conf.ServerName, "err", err)
		}
	}

	time.Sleep(time.Millisecond * 800)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second*10)
	go func() {
		s.GracefulStop()
		trace.GetReporter().Close()
		cancel()
	}()
	<-ctx.Done()
	if errors.Is(context.DeadlineExceeded, ctx.Err()) {
		log.DefaultLog.Errorw("msg", "[server] graceful shutdown failed!", "name", s.conf.ServerName)
		s.Stop()
	} else {
		log.DefaultLog.Infow("msg", "[server] graceful shutdown success!", "name", s.conf.ServerName)
	}
	return nil
}

func (s *Server) GrpcServer() *grpc.Server {
	return s.Server
}

func (s *Server) HealthService() *health.Server {
	return s.healthService
}
