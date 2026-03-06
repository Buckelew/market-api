package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"market-api/internal/app"
	"market-api/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	application, err := app.New(cfg)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}
	defer application.Close()

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           application.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	log.Printf("market-api listening on :%s", cfg.Port)

	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}
