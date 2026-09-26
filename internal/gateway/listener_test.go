package gateway_test

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/gateway"
)

func TestThePortOpensAndClosesWithTheSwitch(t *testing.T) {
	hello := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "gateway") })
	port := gateway.NewPort("127.0.0.1:0", hello)
	t.Cleanup(func() { _ = port.Close() })

	if port.Addr() != "" {
		t.Fatalf("closed port has address %q", port.Addr())
	}
	if err := port.Open(); err != nil {
		t.Fatal(err)
	}
	if err := port.Open(); err != nil {
		t.Fatalf("opening an open port: %v", err)
	}
	addr := port.Addr()
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "gateway" {
		t.Errorf("open port answered %q", body)
	}

	if err := port.Shut(); err != nil {
		t.Fatal(err)
	}
	if _, err := net.Dial("tcp", addr); err == nil {
		t.Error("the port still accepts connections after it was shut")
	}
	if err := port.Shut(); err != nil {
		t.Errorf("shutting a shut port: %v", err)
	}
}

func TestAPortInUseIsExplained(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = busy.Close() })
	port := gateway.NewPort(busy.Addr().String(), http.NotFoundHandler())
	err = port.Open()
	if err == nil || !strings.Contains(err.Error(), "already uses port") {
		t.Errorf("Open on a busy port: %v, want it to say another program uses the port", err)
	}
}
