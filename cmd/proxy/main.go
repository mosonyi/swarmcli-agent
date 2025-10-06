package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func health(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func execBridge(w http.ResponseWriter, r *http.Request) {
	cws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade client ws: %v", err)
		return
	}
	defer cws.Close()

	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		_ = cws.WriteMessage(websocket.TextMessage, []byte("error: need task_id"))
		return
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		_ = cws.WriteMessage(websocket.TextMessage, []byte("error: docker client: "+err.Error()))
		return
	}
	defer cli.Close()

	t, _, err := cli.TaskInspectWithRaw(r.Context(), taskID)
	if err != nil {
		_ = cws.WriteMessage(websocket.TextMessage, []byte("error: inspect task: "+err.Error()))
		return
	}

	nodeID := t.NodeID
	containerID := t.Status.ContainerStatus.ContainerID

	agentIP, err := findAgentIPOnNode(r.Context(), cli, getenv("AGENT_SERVICE", "stack_agent"), getenv("OVERLAY_NAME", "agent-net"), nodeID)
	if err != nil {
		_ = cws.WriteMessage(websocket.TextMessage, []byte("error: resolve agent: "+err.Error()))
		return
	}

	u := url.URL{
		Scheme:   "ws",
		Host:     fmt.Sprintf("%s:8080", agentIP),
		Path:     "/v1/exec",
		RawQuery: url.Values{"container_id": {containerID}, "cmd": {"/bin/sh"}, "tty": {"1"}}.Encode(),
	}
	agws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		_ = cws.WriteMessage(websocket.TextMessage, []byte("error: dial agent: "+err.Error()))
		return
	}
	defer agws.Close()

	done := make(chan struct{})
	go func() {
		for {
			mt, msg, er := agws.ReadMessage()
			if er != nil {
				break
			}
			if er2 := cws.WriteMessage(mt, msg); er2 != nil {
				break
			}
		}
		close(done)
	}()

	for {
		mt, msg, er := cws.ReadMessage()
		if er != nil {
			break
		}
		if er2 := agws.WriteMessage(mt, msg); er2 != nil {
			break
		}
	}

	<-done
}

func logsBridge(w http.ResponseWriter, r *http.Request) {
	cws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade client ws: %v", err)
		return
	}
	defer cws.Close()

	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		_ = cws.WriteMessage(websocket.TextMessage, []byte("error: need task_id"))
		return
	}

	follow := r.URL.Query().Get("follow") == "true"
	tail := r.URL.Query().Get("tail")

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		_ = cws.WriteMessage(websocket.TextMessage, []byte("error: docker client: "+err.Error()))
		return
	}
	defer cli.Close()

	// inspect task to get serviceID
	t, _, err := cli.TaskInspectWithRaw(r.Context(), taskID)
	if err != nil {
		_ = cws.WriteMessage(websocket.TextMessage, []byte("error: inspect task: "+err.Error()))
		return
	}
	serviceID := t.ServiceID

	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
	}

	rc, err := cli.ServiceLogs(r.Context(), serviceID, opts)
	if err != nil {
		_ = cws.WriteMessage(websocket.TextMessage, []byte("error: service logs: "+err.Error()))
		return
	}
	defer rc.Close()

	buf := make([]byte, 32*1024)
	for {
		n, er := rc.Read(buf)
		if n > 0 {
			if er2 := cws.WriteMessage(websocket.BinaryMessage, buf[:n]); er2 != nil {
				break
			}
		}
		if er != nil {
			break
		}
	}
}

func findAgentIPOnNode(ctx context.Context, cli *client.Client, agentService, overlayName, nodeID string) (string, error) {
	f := filters.NewArgs()
	f.Add("service", agentService)
	tasks, err := cli.TaskList(ctx, types.TaskListOptions{Filters: f})
	if err != nil {
		return "", err
	}
	for _, t := range tasks {
		if t.NodeID != nodeID || t.Status.State != swarm.TaskStateRunning {
			continue
		}
		for _, na := range t.NetworksAttachments {
			for _, addr := range na.Addresses {
				if strings.Contains(addr, "/") && na.Network.Spec.Annotations.Name == overlayName {
					return strings.Split(addr, "/")[0], nil
				}
			}
		}
	}
	return "", fmt.Errorf("no running agent task on node %s", nodeID)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/v1/exec", execBridge)
	mux.HandleFunc("/v1/logs", logsBridge)

	addr := getenv("PROXY_LISTEN", ":8443")
	srv := &http.Server{Addr: addr, Handler: mux}

	if cert, key := os.Getenv("PROXY_TLS_CERT"), os.Getenv("PROXY_TLS_KEY"); cert != "" && key != "" {
		caCertPath := os.Getenv("PROXY_CA_CERT")
		if caCertPath == "" {
			log.Fatal("missing PROXY_CA_CERT environment variable")
		}

		caCert, err := os.ReadFile(caCertPath)
		if err != nil {
			log.Fatalf("read CA cert: %v", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			log.Fatal("invalid CA cert")
		}

		tlsConfig := &tls.Config{
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  caPool,
			MinVersion: tls.VersionTLS13,
		}

		srv.TLSConfig = tlsConfig
		log.Printf("proxy TLS (mutual auth) on %s", addr)
		log.Fatal(srv.ListenAndServeTLS(cert, key))
	}

}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
