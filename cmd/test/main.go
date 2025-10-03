package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

func dialWS(u url.URL) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer
	dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	c, _, err := dialer.Dial(u.String(), nil) // discard resp
	return c, err
}

func modeExec(taskID, cmd, proxy string) {
	u := url.URL{
		Scheme:   "wss",
		Host:     proxy[6:],
		Path:     "/v1/exec",
		RawQuery: "task_id=" + taskID + "&cmd=" + url.QueryEscape(cmd) + "&tty=1",
	}
	log.Printf("connecting exec to %s", u.String())

	c, err := dialWS(u)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatal(err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				return
			}
			if string(message) == "EXEC_FINISHED" {
				log.Println("session finished")
				return
			}
			os.Stdout.Write(message)
		}
	}()

	buf := make([]byte, 1024)
	for {
		select {
		case <-done:
			return
		case <-interrupt:
			log.Println("interrupt: closing connection")
			c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		default:
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if err := c.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}
}

func modeLogs(taskID, proxy string, tail int) {
	u := url.URL{
		Scheme:   "wss",
		Host:     proxy[6:],
		Path:     "/v1/logs",
		RawQuery: fmt.Sprintf("task_id=%s&follow=1&tail=%d", taskID, tail),
	}
	log.Printf("connecting logs to %s", u.String())

	c, err := dialWS(u)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				return
			}
			os.Stdout.Write(message)
		}
	}()

	<-interrupt
	log.Println("interrupt: closing logs")
	_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func main() {
	mode := flag.String("mode", "exec", "Mode: exec or logs")
	taskID := flag.String("task", "", "Task ID to target")
	cmd := flag.String("cmd", "hostname", "Command for exec mode")
	proxy := flag.String("proxy", "wss://localhost:8443", "Proxy base URL")
	tail := flag.Int("tail", 10, "Number of log lines for logs mode")
	flag.Parse()

	if *taskID == "" {
		fmt.Println("Usage: test -mode [exec|logs] -task <task_id> [-cmd hostname] [-proxy wss://host:8443]")
		os.Exit(1)
	}

	if *mode == "logs" {
		modeLogs(*taskID, *proxy, *tail)
	} else {
		modeExec(*taskID, *cmd, *proxy)
	}
}
