package main

import (
	"context"
	"time"

	"github.com/tencentyun/tsf-go/pkg/grpc/server"
	"github.com/tencentyun/tsf-go/pkg/log"
	"github.com/tencentyun/tsf-go/pkg/proxy"
	"github.com/tencentyun/tsf-go/pkg/util"
	pb "github.com/tencentyun/tsf-go/testdata"

	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func main() {
	util.ParseFlag()
	proxy.Init()

	server := server.NewServer(&server.Config{ServerName: "provider-demo"})
	pb.RegisterGreeterServer(server.Server, &Service{})
	server.Use(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		start := time.Now()
		resp, err = handler(ctx, req)
		log.L().Info(ctx, "enter grpc handler!", zap.String("method", info.FullMethod), zap.Duration("dur", time.Since(start)))
		return
	})

	err := server.Start()
	if err != nil {
		panic(err)
	}
}

type Service struct {
}

func (s *Service) SayHello(ctx context.Context, req *pb.HelloRequest) (*pb.HelloReply, error) {
	return &pb.HelloReply{Message: "hi " + req.Name}, nil
}