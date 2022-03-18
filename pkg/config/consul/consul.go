package consul

import (
	"context"
	"encoding/base64"
	"fmt"
	xhttp "net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/hisonsoft/tsf-go/log"
	"github.com/hisonsoft/tsf-go/pkg/config"
	"github.com/hisonsoft/tsf-go/pkg/http"
	"github.com/hisonsoft/tsf-go/pkg/sys/env"
	"github.com/hisonsoft/tsf-go/pkg/util"

	"gopkg.in/yaml.v3"
)

var (
	_ config.Source = &Consul{}

	defaultConsul *Consul
	mu            sync.Mutex
)

const separator string = "-"

type Config struct {
	Address string
	Token   string
	// additional message: tsf namespaceid and tencent appid if exsist
	AppID       string
	NamespaceID string
}

type Consul struct {
	queryCli *http.Client
	bc       *util.BackoffConfig
	lock     sync.RWMutex
	conf     *Config

	topic map[string]*Topic
}

func DefaultConsul() *Consul {
	mu.Lock()
	defer mu.Unlock()
	if defaultConsul == nil {
		defaultConsul = New(&Config{
			Address: fmt.Sprintf("%s:%d", env.ConsulHost(), env.ConsulPort()),
			Token:   env.Token(),
		})
	}
	return defaultConsul
}

func New(conf *Config) *Consul {
	return &Consul{
		queryCli: http.NewClient(http.WithTimeout(time.Second * 90)),
		bc: &util.BackoffConfig{
			MaxDelay:  25 * time.Second,
			BaseDelay: 500 * time.Millisecond,
			Factor:    1.5,
			Jitter:    0.2,
		},
		topic: make(map[string]*Topic),
		conf:  conf,
	}
}

func (c *Consul) Subscribe(path string) config.Watcher {
	w := &Watcher{
		event: make(chan struct{}, 1),
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	c.lock.Lock()
	defer c.lock.Unlock()
	topic, ok := c.topic[path]
	if !ok {
		topic = &Topic{
			path:    path,
			consul:  c,
			watcher: make(map[*Watcher]struct{}),
		}
		var ctx context.Context
		ctx, topic.cancel = context.WithCancel(context.Background())
		c.topic[path] = topic
		go topic.subscribe(ctx)
	}
	w.topic = topic
	topic.watcher[w] = struct{}{}
	return w
}

func (c *Consul) fetch(path string, index int64) (res []config.Spec, consulIndex int64, err error) {
	url := fmt.Sprintf("http://%s/v1/kv/%s?token=%s&wait=55s&nsType=DEF_AND_GLOBAL&index=%d", c.conf.Address, path, c.conf.Token, index)
	if strings.HasSuffix(path, "/") {
		url += "&recurse"
	}
	if c.conf.NamespaceID != "" {
		url += "&nid=" + c.conf.NamespaceID
	}
	if c.conf.AppID != "" {
		url += "&uid=" + c.conf.AppID
	}
	defer func() {
		if err != nil {
			log.DefaultLog.Errorw("msg", "[config] get config failed!", "url", url, "err", err)
		}
	}()

	var (
		header xhttp.Header
		items  []struct {
			Key   string
			Value string
		}
	)
	header, err = c.queryCli.Get(url, &items)
	if err != nil {
		if errors.IsNotFound(err) {
			err = nil
		} else {
			return
		}
	}
	if header != nil {
		str := header.Get("X-Consul-Index")
		consulIndex, err = strconv.ParseInt(str, 10, 64)
		if err != nil {
			err = errors.InternalServer(errors.UnknownReason, fmt.Sprintf("consul index invalid: %s", str))
			return
		}
	} else {
		err = errors.InternalServer(errors.UnknownReason, "consul index invalid,no http header found!")
		return
	}
	for _, item := range items {
		b, err := base64.StdEncoding.DecodeString(item.Value)
		if err != nil {
			log.DefaultLog.Errorw("msg", "[config] fetch failed!", "url", url, "key", item.Key, "value", item.Value, "err", err)
			continue
		}
		res = append(res, config.Spec{Key: item.Key, Data: raw(b)})
	}
	return
}

func (t *Topic) subscribe(ctx context.Context) {
	var (
		lastIndex int64
		lastRes   []config.Spec
		err       error
	)
	lastRes, lastIndex, err = t.consul.fetch(t.path, lastIndex)
	if err == nil {
		t.broadcast(lastRes)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			res, index, err := t.consul.fetch(t.path, lastIndex)
			if err != nil {
				continue
			}
			if !reflect.DeepEqual(lastRes, res) {
				t.broadcast(res)
			}
			lastRes = res
			lastIndex = index
		}
	}
}

func (t *Topic) broadcast(res []config.Spec) {
	t.spec.Store(res)
	t.consul.lock.Lock()
	defer t.consul.lock.Unlock()
	for k := range t.watcher {
		select {
		case k.event <- struct{}{}:
		default:
		}
	}
}

func (c *Consul) Get(ctx context.Context, path string) (spec []config.Spec) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	v, ok := c.topic[path]
	if !ok {
		return
	}
	spec, _ = v.spec.Load().([]config.Spec)
	return nil
}

type Topic struct {
	path    string
	spec    atomic.Value
	cancel  context.CancelFunc
	consul  *Consul
	watcher map[*Watcher]struct{}
}

type raw []byte

func (r raw) Unmarshal(out interface{}) error {
	if r == nil {
		return nil
	}
	return yaml.Unmarshal(r, out)
}

func (r raw) Raw() []byte {
	if r == nil {
		return nil
	}
	return r
}

type Watcher struct {
	topic  *Topic
	event  chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
}

func (w *Watcher) Watch(ctx context.Context) (spec []config.Spec, err error) {
	select {
	case <-ctx.Done():
		err = errors.GatewayTimeout(errors.UnknownReason, "")
		return
	case <-w.ctx.Done():
		err = errors.ClientClosed(errors.UnknownReason, "")
		return
	case <-w.event:
		spec, _ = w.topic.spec.Load().([]config.Spec)
	}
	return
}

func (w *Watcher) Close() {
	select {
	case <-w.ctx.Done():
		return
	default:
	}
	w.cancel()
	w.topic.consul.lock.Lock()
	defer w.topic.consul.lock.Unlock()
	delete(w.topic.watcher, w)
	if len(w.topic.watcher) == 0 {
		delete(w.topic.consul.topic, w.topic.path)
		w.topic.cancel()
	}
}
