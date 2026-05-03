package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/basili4-1982/api-gateway/internal/config"
	"github.com/basili4-1982/api-gateway/internal/logger"
	"github.com/basili4-1982/api-gateway/internal/proxy"
)

func main() {
	configPath := flag.String("config", "/etc/proxy/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	log, err := logger.NewZapLogger(&cfg.Logging)
	if err != nil {
		_, err := fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		if err != nil {
			return
		}
		os.Exit(1)
	}
	defer func(log *zap.Logger) {
		err := log.Sync()
		if err != nil {
			_, err := fmt.Fprintf(os.Stderr, "Failed to sync logger: %v\n", err)
			if err != nil {
				return
			}
		}
	}(log)

	p, err := proxy.NewMultiProxy(cfg, log)
	if err != nil {
		log.Error("Failed to create proxy", zap.Error(err))
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	serverErrors := make(chan error, 1)

	go func() {
		log.Info("Multi-target proxy server starting...")
		if err := p.Start(); err != nil {
			serverErrors <- err
		}
	}()

	log.Info("Multi-target JWT Proxy is running.",
		zap.Int("port", cfg.Server.Port),
		zap.Int("targets", len(cfg.Targets)),
	)

	for {
		select {
		case err := <-serverErrors:
			log.Error("Proxy server failed", zap.Error(err))
			os.Exit(1)

		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				log.Info("Received SIGHUP, reloading configuration...")
				newCfg, err := config.Load(*configPath)
				if err != nil {
					log.Error("Failed to reload config", zap.Error(err))
					continue
				}
				if err := p.Reload(newCfg); err != nil {
					log.Error("Failed to apply reloaded config", zap.Error(err))
				}
			default:
				log.Info("Received shutdown signal", zap.String("signal", sig.String()))
				goto shutdown
			}
		}
	}

shutdown:
	log.Info("Shutting down multi-target JWT Proxy...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := p.Stop(shutdownCtx); err != nil {
		log.Error("Error during shutdown", zap.Error(err))
	}

	log.Info("Proxy server stopped")
}
