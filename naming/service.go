package naming

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-kratos/kratos/v2/registry"
	"github.com/hisonsoft/tsf-go/pkg/sys/env"
)

const (
	StatusUp   = 0
	StatusDown = 1

	GroupID       = "TSF_GROUP_ID"
	NamespaceID   = "TSF_NAMESPACE_ID"
	ApplicationID = "TSF_APPLICATION_ID"
	Region        = "TSF_REGION"

	NsLocal  = "local"
	NsGlobal = "global"
)

// Service 服务信息
type Service struct {
	Namespace string
	Name      string
}

func NewService(namespace string, name string) *Service {
	if namespace == "" || namespace == NsLocal {
		namespace = env.NamespaceID()
	}
	return &Service{Namespace: namespace, Name: name}
}

// Instance 服务实例信息
type Instance struct {
	// 服务信息
	Service *Service `json:"service,omitempty"`
	// namespace下全局唯一的实例ID
	ID string `json:"id"`
	// 服务实例所属地域
	Region string `json:"region"`
	// 服务实例可访问的ip地址
	Host string `json:"host"`
	// 协议端口
	Port int `json:"port"`
	// 服务实例标签元信息,比如appVersion、group、weight等
	Metadata map[string]string `json:"metadata"`
	// 实例运行状态: up/down
	Status int64 `json:"status"`
	// 过滤用的标签
	Tags []string `json:"tags"`
}

func (i Instance) Addr() string {
	return i.Host + ":" + strconv.FormatInt(int64(i.Port), 10)
}

func (i Instance) ToKratosInstance() *registry.ServiceInstance {
	metadata := make(map[string]string)
	for k, v := range i.Metadata {
		metadata[k] = v
	}
	metadata["tsf_status"] = strconv.FormatInt(i.Status, 10)
	tags, _ := json.Marshal(i.Tags)
	metadata["tsf_tags"] = string(tags)
	protocol := metadata["protocol"]
	if protocol == "" {
		protocol = "http"
	}
	ki := &registry.ServiceInstance{
		ID:        i.ID,
		Name:      i.Service.Name,
		Version:   metadata["TSF_PROG_VERSION"],
		Metadata:  metadata,
		Endpoints: []string{fmt.Sprintf("%s://%s:%d", protocol, i.Host, i.Port)},
	}
	return ki
}

func FromKratosInstance(ki *registry.ServiceInstance) (inss []*Instance) {
	for _, e := range ki.Endpoints {
		scheme, ip, port := parseEndpoint(e)
		status, _ := strconv.Atoi(ki.Metadata["tsf_status"])
		id := ki.ID
		if len(ki.Endpoints) > 1 {
			id += "-" + scheme
		}
		ins := &Instance{
			Service:  &Service{Namespace: ki.Metadata[NamespaceID], Name: ki.Name + "_" + scheme},
			ID:       id,
			Region:   ki.Metadata[Region],
			Host:     ip,
			Port:     port,
			Metadata: ki.Metadata,
			Status:   int64(status),
		}
		ins.Metadata = make(map[string]string)
		for k, v := range ki.Metadata {
			ins.Metadata[k] = v
		}
		ins.Metadata["protocol"] = scheme
		if scheme == "grpc" {
			if ins.Metadata["TSF_API_METAS_GRPC"] != "" {
				ins.Metadata["TSF_API_METAS"] = ins.Metadata["TSF_API_METAS_GRPC"]
			}
		} else if ins.Metadata["TSF_API_METAS_HTTP"] != "" {
			ins.Metadata["TSF_API_METAS"] = ins.Metadata["TSF_API_METAS_HTTP"]
		}
		delete(ins.Metadata, "TSF_API_METAS_GRPC")
		delete(ins.Metadata, "TSF_API_METAS_HTTP")
		json.Unmarshal([]byte(ki.Metadata["tsf_tags"]), &ins.Tags)
		inss = append(inss, ins)
	}
	return
}

func parseEndpoint(endpoint string) (string, string, int) {
	u, _ := url.Parse(endpoint)
	addrs := strings.Split(u.Host, ":")
	ip := addrs[0]
	port, _ := strconv.ParseInt(addrs[1], 10, 32)
	return u.Scheme, ip, int(port)
}
