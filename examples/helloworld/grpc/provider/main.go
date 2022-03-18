package main

import (
	"context"
	"flag"
	"fmt"

	tsf "github.com/hisonsoft/tsf-go"
	pb "github.com/hisonsoft/tsf-go/examples/helloworld/proto"
	"github.com/hisonsoft/tsf-go/log"

	"github.com/go-kratos/kratos/v2"
	klog "github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/grpc"
)

// server is used to implement helloworld.GreeterServer.
type server struct {
	l *klog.Helper
	pb.UnimplementedGreeterServer
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	s.l.Infof("recv name:%s", in.Name)
	return &pb.HelloReply{Message: fmt.Sprintf("Welcome %+v!", in.Name)}, nil
}

func main() {
	flag.Parse()
	logger := log.DefaultLogger
	log := log.NewHelper(logger)

	grpcSrv := grpc.NewServer(
		grpc.Address(":9000"),
		grpc.Middleware(
			recovery.Recovery(),
			tsf.ServerMiddleware(),
			logging.Server(logger),
		),
	)
	s := &server{l: log}
	pb.RegisterGreeterServer(grpcSrv, s)

	opts := []kratos.Option{kratos.Name("provider-grpc"), kratos.Server(grpcSrv)}
	opts = append(opts, tsf.AppOptions()...)
	app := kratos.New(opts...)
	if err := app.Run(); err != nil {
		log.Errorf("app run failed:%v", err)
	}
}
