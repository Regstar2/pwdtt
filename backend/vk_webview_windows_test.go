//go:build windows

package backend

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWaitForPageTargetAllowsEdgeLauncherHandoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/list" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"type":"page","url":"https://vk.ru/","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/page/1"}]`))
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	session := &vkEdgeSession{
		waitCh: make(chan error, 1),
		port:   port,
		http:   server.Client(),
	}
	session.waitCh <- nil
	session.closed.Store(true)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	target, err := session.waitForPageTarget(ctx)
	if err != nil {
		t.Fatalf("launcher handoff must not abort CDP discovery: %v", err)
	}
	if target.WebSocketDebuggerURL == "" {
		t.Fatal("expected a page target after launcher handoff")
	}
}

func TestVKEdgeSessionUsesCDPStateAfterLauncherHandoff(t *testing.T) {
	session := &vkEdgeSession{conn: &websocket.Conn{}}
	session.closed.Store(true)

	if session.exited() {
		t.Fatal("launcher exit must not mark an attached CDP browser as exited")
	}

	session.cdpClosed.Store(true)
	if !session.exited() {
		t.Fatal("CDP closure must mark the browser session as exited")
	}
}
