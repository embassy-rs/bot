package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"embassy.dev/bot/toolkit/log"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sqlbunny/errors"
)

type Server struct {
	Name       string
	Router     func(chi.Router)
	Handler    http.Handler
	Middleware []func(http.Handler) http.Handler
	Timeout    time.Duration
	Config     *Config

	server *http.Server
}

func (s *Server) buildHandler() http.Handler {
	if s.Handler != nil {
		return s.Handler
	}

	r := chi.NewRouter()

	// Sub-router, so /healthz below doesn't go through the middleware. We don't
	// want kubelet's probes spamming the logs.
	r2 := r.With()
	r2.Use(middleware.RequestID)
	r2.Use(RealIP)
	for _, m := range s.Middleware {
		r2.Use(m)
	}
	r2.Use(s.loggerRecoverer)

	s.Router(r2.With(middleware.Timeout(s.Timeout)))

	r.Get("/healthz", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("ok"))
	})

	return r
}

func (s *Server) Run(ctx context.Context) error {
	handler := s.buildHandler()

	portNum, ok := s.Config.Ports[s.Name]
	if !ok {
		return errors.Errorf("Port number not found in CONFIG_PORTS for %s", s.Name)
	}

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", portNum),
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(1)

	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
			_ = s.server.Shutdown(context.Background())
			wg.Done()
		}
	}()

	log.Infof(ctx, "listening", log.Fields{"name": s.Name, "port": portNum})

	err := s.server.ListenAndServe()
	if err != http.ErrServerClosed {
		close(done) // Un-hang the goroutine
		return errors.Errorf("Error starting server for %s (port %d): %w", s.Name, portNum, err)
	}

	// If we're here, the server is shutting down. Wait for Shutdown() to return
	wg.Wait()

	return nil
}

func MetricsServer(config *Config) *Server {
	return &Server{
		Name:    "metrics",
		Config:  config,
		Handler: promhttp.Handler(),
	}
}
