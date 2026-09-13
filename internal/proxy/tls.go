package proxy

import (
	"fmt"
	"net/http"
	"path/filepath"

	"go.uber.org/zap"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/basili4-1982/api-gateway/internal/config"
)

// letsEncryptStagingDirectory — ACME directory тестового CA Let's Encrypt.
const letsEncryptStagingDirectory = "https://acme-staging-v02.api.letsencrypt.org/directory"

// buildCertManager собирает autocert.Manager по TLS-конфигу. При staging
// используется ACME staging directory и отдельный cache-подкаталог, чтобы
// staging-сертификаты не смешивались с production. tls.directory_url
// перекрывает выбор CA.
func buildCertManager(tls *config.TLSConfig) *autocert.Manager {
	cacheDir := tls.CacheDir
	if tls.Staging {
		cacheDir = filepath.Join(cacheDir, "staging")
	}

	manager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(tls.Domains...),
		Cache:      autocert.DirCache(cacheDir),
		Email:      tls.Email,
	}

	directoryURL := tls.DirectoryURL
	if directoryURL == "" && tls.Staging {
		directoryURL = letsEncryptStagingDirectory
	}
	if directoryURL != "" {
		manager.Client = &acme.Client{DirectoryURL: directoryURL}
	}

	return manager
}

func (mp *MultiProxy) startTLS() error {
	tls := mp.config.Load().TLS

	certManager := buildCertManager(tls)

	tlsServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", tls.Port),
		Handler:      mp,
		ReadTimeout:  mp.config.Load().Server.ReadTimeout,
		WriteTimeout: mp.config.Load().Server.WriteTimeout,
		IdleTimeout:  mp.config.Load().Server.IdleTimeout,
		TLSConfig:    certManager.TLSConfig(),
	}
	mp.httpsServer = tlsServer

	httpHandler := certManager.HTTPHandler(mp.httpRedirectHandler())
	httpServer := &http.Server{
		Addr:        fmt.Sprintf(":%d", tls.HTTPPort),
		Handler:     httpHandler,
		ReadTimeout: mp.config.Load().Server.ReadTimeout,
		IdleTimeout: mp.config.Load().Server.IdleTimeout,
	}
	mp.httpServer = httpServer

	mp.logger.Info("Starting HTTPS proxy server with auto TLS",
		zap.Int("https_port", tls.Port),
		zap.Int("http_port", tls.HTTPPort),
		zap.Strings("domains", tls.Domains),
		zap.String("email", tls.Email),
		zap.String("cache_dir", tls.CacheDir),
		zap.Bool("staging", tls.Staging),
		zap.String("directory_url", tls.DirectoryURL),
		zap.Int("targets", len(mp.targets)),
	)

	mp.logTargets()

	errCh := make(chan error, 2)

	go func() {
		mp.logger.Info("Starting HTTP server (ACME challenge + redirect)",
			zap.Int("port", tls.HTTPPort),
		)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()

	go func() {
		mp.logger.Info("Starting HTTPS server",
			zap.Int("port", tls.Port),
		)
		if err := tlsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("HTTPS server error: %w", err)
		}
	}()

	return <-errCh
}
