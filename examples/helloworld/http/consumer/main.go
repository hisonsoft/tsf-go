package main

import (
	"context"
	"flag"
	"log"
	"time"

	transhttp "github.com/go-kratos/kratos/v2/transport/http"
	pb "github.com/hisonsoft/tsf-go/examples/helloworld/proto"
	"github.com/hisonsoft/tsf-go/naming/consul"
)

func main() {
	flag.Parse()

	c := consul.DefaultConsul()

	go func() {
		for {
			time.Sleep(time.Millisecond * 1000)
			callHTTP()
			time.Sleep(time.Second)
		}
	}()

	newService(c)
}

func callHTTP() {
	conn, err := transhttp.NewClient(
		context.Background(),
		transhttp.WithEndpoint("127.0.0.1:8080"),
	)
	if err != nil {
		panic(err)
	}
	client := pb.NewGreeterHTTPClient(conn)
	reply, err := client.SayHello(context.Background(), &pb.HelloRequest{Name: "kratos_http"})
	if err != nil {
		panic(err)
	}
	log.Printf("[http] SayHello %s\n", reply.Message)
}
