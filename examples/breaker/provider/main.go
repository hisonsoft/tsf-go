package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/transport/http"
	tsf "github.com/hisonsoft/tsf-go"
	pb "github.com/hisonsoft/tsf-go/examples/helloworld/proto"
	"github.com/hisonsoft/tsf-go/log"
)

// server is used to implement helloworld.GreeterServer.
type server struct {
	pb.UnimplementedGreeterServer
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.HelloRequest) (*pb.HelloReply, error) {
	if in.Name == "error" {
		return nil, errors.Forbidden("provider_error", "")
	}
	return &pb.HelloReply{Message: fmt.Sprintf("Welcome %+v!", in.Name)}, nil
}

func main() {
	flag.Parse()
	logger := log.DefaultLogger
	log := log.NewHelper(logger)

	s := &server{}
	httpSrv := http.NewServer(
		http.Address("0.0.0.0:8000"),
		http.Middleware(
			recovery.Recovery(),
			tsf.ServerMiddleware(),
			logging.Server(logger),
		),
	)
	pb.RegisterGreeterHTTPServer(httpSrv, s)

	opts := []kratos.Option{kratos.Name("provider-http"), kratos.Server(httpSrv)}
	opts = append(opts, tsf.AppOptions(tsf.EnableReigstry(false))...)
	app := kratos.New(opts...)

	if err := app.Run(); err != nil {
		log.Errorf("app run failed:%v", err)
	}
}
