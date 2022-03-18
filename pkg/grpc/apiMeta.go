package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fullstorydev/grpcurl"
	"github.com/hisonsoft/tsf-go/log"
	"github.com/hisonsoft/tsf-go/pkg/sys/apiMeta"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

func GetServiceMethods(addr string) (serDesc map[string]*apiMeta.Service, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		panic(fmt.Errorf("dail grpc server failed,process exit now!addr:=%v err:=%v", addr, err))
	}
	cli := rpb.NewServerReflectionClient(conn)
	refClient := grpcreflect.NewClient(ctx, cli)
	reflSource := grpcurl.DescriptorSourceFromServer(ctx, refClient)
	services, err := reflSource.ListServices()
	if err != nil {
		return
	}
	serDesc = make(map[string]*apiMeta.Service, 0)
	for _, service := range services {
		if service == "grpc.reflection.v1alpha.ServerReflection" {
			continue
		}
		desc, err := reflSource.FindSymbol(service)
		if err != nil {
			log.DefaultLog.Errorw("msg", "FindSymbol failed!", "symbol", service, "err", err)
			continue
		}
		for _, s := range desc.GetFile().GetServices() {
			packageName := desc.GetFile().GetPackage()
			serviceName := strings.TrimPrefix(service, packageName+".")
			if serviceName == s.GetName() {
				desc := &apiMeta.Service{PackageName: packageName, ServiceName: serviceName}
				for _, method := range s.GetMethods() {
					desc.Paths = append(desc.Paths, apiMeta.Path{FullName: fmt.Sprintf("/%s/%s", service, method.GetName()), Method: "post"})
				}
				serDesc[service] = desc
			}
		}
	}
	return
}
