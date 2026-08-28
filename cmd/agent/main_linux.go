//go:build linux

package main

import (
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gpu-scheduler/internal/agentserver"
	"gpu-scheduler/internal/config"
)

func main() {
	configPath := flag.String("config", "/etc/gpu-scheduler/config.toml", "path to config.toml")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Agent.SocketPath), 0o750); err != nil {
		log.Error("create socket directory", "error", err)
		os.Exit(1)
	}
	_ = os.Remove(cfg.Agent.SocketPath)
	listener, err := net.Listen("unix", cfg.Agent.SocketPath)
	if err != nil {
		log.Error("listen on agent socket", "error", err)
		os.Exit(1)
	}
	if err := os.Chmod(cfg.Agent.SocketPath, 0o660); err != nil {
		log.Error("set socket permissions", "error", err)
		os.Exit(1)
	}

	agent, err := agentserver.New(cfg)
	if err != nil {
		log.Error("initialize agent", "error", err)
		os.Exit(1)
	}
	defer agent.Close()
	server := &http.Server{Handler: agent.Handler(), ReadHeaderTimeout: 5 * time.Second}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		log.Info("agent listening", "socket", cfg.Agent.SocketPath)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Error("agent server", "error", err)
			os.Exit(1)
		}
	}()
	<-stop
	_ = server.Close()
}
