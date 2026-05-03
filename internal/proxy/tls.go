package proxy

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"
	"golang.org/x/crypto/acme/autocert"
)

func (mp *MultiProxy) startTLS() error {
	tls := mp.config.TLS

	certManager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(tls.Domains...),
		Cache:      autocert.DirCache(tls.CacheDir),
		Email:      tls.Email,
	}

	tlsServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", tls.Port),
		Handler:      mp,
		ReadTimeout:  mp.config.Server.ReadTimeout,
		WriteTimeout: mp.config.Server.WriteTimeout,
		IdleTimeout:  mp.config.Server.IdleTimeout,
		TLSConfig:    certManager.TLSConfig(),
	}
	mp.httpsServer = tlsServer

	httpHandler := certManager.HTTPHandler(mp.httpRedirectHandler())
	httpServer := &http.Server{
		Addr:        fmt.Sprintf(":%d", tls.HTTPPort),
		Handler:     httpHandler,
		ReadTimeout: mp.config.Server.ReadTimeout,
		IdleTimeout: mp.config.Server.IdleTimeout,
	}
	mp.httpServer = httpServer

	mp.logger.Info("Starting HTTPS proxy server with auto TLS",
		zap.Int("https_port", tls.Port),
		zap.Int("http_port", tls.HTTPPort),
		zap.Strings("domains", tls.Domains),
		zap.String("email", tls.Email),
		zap.String("cache_dir", tls.CacheDir),
		zap.Bool("staging", tls.Staging),
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
