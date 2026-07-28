package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"embassy.dev/bot/toolkit/log"
	"embassy.dev/bot/toolkit/nopanic"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sqlbunny/errors"
)

var (
	requests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"handler", "method", "code"},
	)
	requestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Histogram of latencies for HTTP requests.",
			Buckets: []float64{.05, 0.1, .25, .5, .75, 1, 2, 5, 20, 60},
		},
		[]string{"handler", "method"},
	)
)

func (s *Server) loggerRecoverer(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		ctx := r.Context()
		method := strings.ToLower(r.Method)
		rid := middleware.GetReqID(ctx)
		ctx = log.With(ctx, log.Fields{
			"request_id": rid,
			"request": map[string]any{
				"port":     s.Name,
				"method":   method,
				"url":      r.URL.String(),
				"remoteIp": r.RemoteAddr,
			},
		})
		ww.Header().Set("X-Request-ID", rid)

		t1 := time.Now()
		defer func() {
			routePattern := chi.RouteContext(ctx).RoutePattern()
			duration := time.Since(t1)
			requests.WithLabelValues(routePattern, method, strconv.Itoa(ww.Status())).Inc()
			requestDuration.WithLabelValues(routePattern, method).Observe(duration.Seconds())

			log.Infof(ctx, "response", log.Fields{
				"status": ww.Status(),
				"bytes":  ww.BytesWritten(),
				"t":      duration,
			})
		}()

		err := nopanic.Run(func() error {
			next.ServeHTTP(ww, r.WithContext(ctx))
			return nil
		})
		if err != nil {
			log.Error(ctx, errors.StackTrace(err))
			// The handler may have written a partial response already, in which
			// case WriteHeader is a no-op and logs a warning. Nothing better to
			// do: the status is already on the wire.
			ww.WriteHeader(http.StatusInternalServerError)
		}
	}
	return http.HandlerFunc(fn)
}
