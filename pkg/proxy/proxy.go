package proxy

import (
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hisonsoft/tsf-go/log"
	"github.com/hisonsoft/tsf-go/pkg/sys/env"

	"github.com/elazarl/goproxy"
	"golang.org/x/crypto/ssh"
)

var (
	client   *ssh.Client
	listener []net.Listener
	mu       sync.Mutex
	inited   bool
)

func Inited() bool {
	mu.Lock()
	defer mu.Unlock()
	return inited
}

func Init() {
	mu.Lock()
	if inited {
		return
	}
	mu.Unlock()

	if env.SSHHost() == "" || env.SSHUser() == "" {
		log.DefaultLog.Infof("no ssh_host & ssh_user detected,proxy tunnel exit!")
		return
	}
	var auths []ssh.AuthMethod
	if env.SSHPass() != "" {
		auths = append(auths, ssh.Password(env.SSHPass()))
	}
	if env.SSHPass() == "" && env.SSHKey() != "" {
		key, err := ioutil.ReadFile(env.SSHKey())
		if err != nil {
			log.DefaultLog.Errorf("unable to read private key: %v", err)
			return
		}
		// Create the Signer for this private key.
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			log.DefaultLog.Errorf("unable to parse private key: %v", err)
			return
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}

	config := &ssh.ClientConfig{
		User: env.SSHUser(),
		Auth: auths,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
		Timeout: 6 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", env.SSHHost(), env.SSHPort())
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		log.DefaultLog.Errorf("unable to connect to ssh host [%s]: %v", addr, err)
		return
	}
	client = conn
	prxy := goproxy.NewProxyHttpServer()
	prxy.Tr = &http.Transport{Dial: client.Dial}
	proxyPort := rand.Int31n(55535) + 10000
	proxyAddr := fmt.Sprintf("0.0.0.0:%d", proxyPort)
	os.Setenv("HTTP_PROXY", fmt.Sprintf("http://127.0.0.1:%d", proxyPort))
	os.Setenv("HTTPS_PROXY", fmt.Sprintf("https://127.0.0.1:%d", proxyPort))
	log.DefaultLog.Infof("listening for local HTTP PROXY connections on [%s]", proxyAddr)
	go func() {
		err = http.ListenAndServe(proxyAddr, prxy)
		log.DefaultLog.Infof("proxy ListenAndServe exit with err: %v", err)
	}()
	mu.Lock()
	inited = true
	client = conn
	mu.Unlock()
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	for _, l := range listener {
		l.Close()
	}
	client.Close()
}

func ListenRemote(lPort int, rPort int) {
	// Request the remote side to open port 8080 on all interfaces.
	log.DefaultLog.Infof("[ListenRemote] listening for remote conn on [:%d] and local conn on [:%d]", rPort, lPort)
	l, err := client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", rPort))
	if err != nil {
		log.DefaultLog.Errorf("[ListenRemote] unable to register tcp forward,err: %v", err)
		return
	}
	mu.Lock()
	listener = append(listener, l)
	mu.Unlock()
	for {
		conn, err := l.Accept()
		if err != nil {
			log.DefaultLog.Errorf("[ListenRemote] accept remote failed,err: %v", err)
			return
		}
		go serveTcp(conn, lPort)
	}
}

func serveTcp(conn net.Conn, localPort int) {
	defer conn.Close()
	localConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		log.DefaultLog.Errorf("[serveTcp] dial local addr 127.0.0.1:%d failed!err:=%v", localPort, err)
		return
	}
	defer localConn.Close()
	ch := make(chan struct{}, 0)
	go func() {
		io.Copy(conn, localConn)
		close(ch)
	}()
	io.Copy(localConn, conn)
	<-ch
}
