package main

import (
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"ts":     time.Now().UTC().Format(time.RFC3339),
	})
}

func execWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer ws.Close()

	containerID := r.URL.Query().Get("container_id")
	if containerID == "" {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("error: missing container_id"))
		return
	}

	cmd := r.URL.Query().Get("cmd")
	if cmd == "" {
		cmd = "/bin/sh"
	}
	args := strings.Fields(cmd)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("error: docker client: "+err.Error()))
		return
	}
	defer cli.Close()

	if _, err := cli.ContainerInspect(r.Context(), containerID); err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("error: inspect: "+err.Error()))
		return
	}

	execCfg := types.ExecConfig{
		Cmd:          args,
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}
	ex, err := cli.ContainerExecCreate(r.Context(), containerID, execCfg)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("error: create exec: "+err.Error()))
		return
	}

	att, err := cli.ContainerExecAttach(r.Context(), ex.ID, types.ExecStartCheck{Tty: true})
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("error: attach exec: "+err.Error()))
		return
	}
	defer att.Close()

	_ = ws.WriteMessage(websocket.TextMessage, []byte("EXEC_ATTACHED"))

	done := make(chan struct{})

	// container -> client
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, er := att.Reader.Read(buf)
			if n > 0 {
				if er2 := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); er2 != nil {
					break
				}
			}
			if er != nil {
				break
			}
		}
		close(done)
	}()

	// client -> container
	go func() {
		for {
			mt, p, er := ws.ReadMessage()
			if er != nil {
				break
			}
			if mt == websocket.TextMessage || mt == websocket.BinaryMessage {
				if _, er2 := att.Conn.Write(p); er2 != nil {
					break
				}
			}
		}
	}()

	// lifecycle monitor
	for {
		select {
		case <-done:
			inspect, err := cli.ContainerExecInspect(r.Context(), ex.ID)
			if err == nil && !inspect.Running {
				_ = ws.WriteMessage(websocket.TextMessage, []byte("EXEC_FINISHED"))
			}
			return
		case <-time.After(1 * time.Second):
			inspect, err := cli.ContainerExecInspect(r.Context(), ex.ID)
			if err == nil && !inspect.Running {
				_ = ws.WriteMessage(websocket.TextMessage, []byte("EXEC_FINISHED"))
				return
			}
		}
	}
}

type wsWriter struct{ ws *websocket.Conn }

func (w wsWriter) Write(p []byte) (int, error) {
	if err := w.ws.WriteMessage(websocket.TextMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func logsWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer ws.Close()

	containerID := r.URL.Query().Get("container_id")
	if containerID == "" {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("error: missing container_id"))
		return
	}
	follow := r.URL.Query().Get("follow") == "1"
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("error: docker client: "+err.Error()))
		return
	}
	defer cli.Close()

	opts := container.LogsOptions{ShowStdout: true, ShowStderr: true, Follow: follow, Tail: tail}
	rc, err := cli.ContainerLogs(r.Context(), containerID, opts)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("error: logs: "+err.Error()))
		return
	}
	defer rc.Close()

	// demux Docker's log stream headers -> plain text lines
	_, _ = stdcopy.StdCopy(wsWriter{ws}, wsWriter{ws}, rc)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/v1/exec", execWS)
	mux.HandleFunc("/v1/logs", logsWS)

	addr := getenv("AGENT_LISTEN", ":8080")
	srv := &http.Server{Addr: addr, Handler: mux}

	if cert, key := os.Getenv("AGENT_TLS_CERT"), os.Getenv("AGENT_TLS_KEY"); cert != "" && key != "" {
		if srv.TLSConfig == nil {
			srv.TLSConfig = &tls.Config{}
		}
		log.Printf("agent TLS on %s", addr)
		log.Fatal(srv.ListenAndServeTLS(cert, key))
	} else {
		log.Printf("agent on %s", addr)
		log.Fatal(srv.ListenAndServe())
	}
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
