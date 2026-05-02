// sockbridge forwards TCP connections to a unix domain socket.
// Usage: sockbridge -listen 127.0.0.1:8181 -socket /run/credproxy/credproxy.sock
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/takezoh/credproxy/bridge"
)

func main() {
	listen := flag.String("listen", "", "TCP address to listen on (e.g. 127.0.0.1:8181)")
	socket := flag.String("socket", "", "unix socket path to forward to")
	flag.Parse()

	if *listen == "" || *socket == "" {
		slog.Error("sockbridge: -listen and -socket are required")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := bridge.Run(ctx, *listen, *socket); err != nil {
		slog.Error("sockbridge: fatal", "err", err)
		os.Exit(1)
	}
}
