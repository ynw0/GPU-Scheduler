package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gpu-scheduler/internal/agentapi"
	"gpu-scheduler/internal/auth"
	"gpu-scheduler/internal/config"
	"gpu-scheduler/internal/controller"
	"gpu-scheduler/internal/notification"
	"gpu-scheduler/internal/store"
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
	ctx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	db, err := store.Open(ctx, cfg.Controller.DatabasePath)
	if err != nil {
		log.Error("open store", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	agent := agentapi.NewClient(cfg.Controller.AgentSocket)
	authService := auth.NewService(db,
		time.Duration(cfg.Security.SessionIdleHours)*time.Hour,
		time.Duration(cfg.Security.SessionAbsoluteHours)*time.Hour,
		cfg.Security.PasswordMinLength)
	notifier := notification.NewWorker(db, cfg.Notifications, time.Duration(cfg.Controller.WebhookTimeoutSeconds)*time.Second)
	go notifier.Run(ctx)
	app := controller.New(log, cfg, db, agent, authService, notifier)
	go app.Start(ctx)
	httpServer := &http.Server{
		Addr:              cfg.Controller.ListenAddress,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		log.Info("controller listening", "address", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("controller server", "error", err)
			os.Exit(1)
		}
	}()
	<-stop
	cancelRun()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
}
