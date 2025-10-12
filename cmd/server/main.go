package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	_shutdownPeriod      = 15 * time.Second
	_shutdownHardPeriod  = 3 * time.Second
	_readinessDrainDelay = 5 * time.Second
)

var (
	Version     string
	Commit      string
	gitLogGroup = slog.Group("git",
		slog.String("version", Version),
		slog.String("commit", Commit),
	)
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	slog.Info("Running server", gitLogGroup)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ongoingCtx, stopOngoingGracefully := context.WithCancel(context.Background())

	h, unaliveServer, cleanup := NewRoutes(ongoingCtx)
	s := NewServer(ongoingCtx, os.Getenv("HOST"), os.Getenv("PORT"), h)

	go func() {
		slog.Info("Starting server", slog.String("addr", s.Addr))

		if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	<-rootCtx.Done()
	stop()
	unaliveServer()

	slog.Info("Received signal. Shutting down.")

	time.Sleep(_readinessDrainDelay)

	slog.Info("Readiness check propagated, now waiting for ongoing requests to finish.")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), _shutdownPeriod)
	defer cancel()

	err := s.Shutdown(shutdownCtx)
	stopOngoingGracefully()
	if err != nil {
		slog.Error("Failed to wait for ongoing requests to finish, waiting for forced cancellation.")
		time.Sleep(_shutdownHardPeriod)
	}

	slog.Info("Server shut down gracefully")

	slog.Info("Cleaning up dependencies...")
	cleanup(shutdownCtx)
}

func NewRoutes(ctx context.Context) (handler http.Handler, unaliceServer func(), cleanup func(context.Context)) {
	cleanupTracer := newTracer(ctx)
	tp := otel.GetTracerProvider()
	mux := http.NewServeMux()

	rh := NewReadinessHandler(tp.Tracer("http.handler.readiness"))

	mux.Handle("GET /health", rh)

	go produceTraces(ctx, tp.Tracer("dummy-trace-generator"))

	return mux, rh.MakeUnavailable, func(ctx context.Context) {
		logError := func(msg string, err error) {
			status := " succeeded"
			if err != nil {
				status = " failed"
			}
			slog.Error(msg+status, "err", err)
		}

		err := cleanupTracer(ctx)
		logError("cleanup tracer", err)
	}
}

func NewServer(embedCtx context.Context, host, port string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    net.JoinHostPort(host, port),
		Handler: handler,
		BaseContext: func(_ net.Listener) context.Context {
			return embedCtx
		},
	}
}

type ReadinessHandler struct {
	available *atomic.Bool
	trace     trace.Tracer
}

func NewReadinessHandler(trace trace.Tracer) *ReadinessHandler {
	available := atomic.Bool{}
	available.Store(true)
	return &ReadinessHandler{
		available: &available,
		trace:     trace,
	}
}

func (r *ReadinessHandler) MakeUnavailable() {
	r.available.Store(false)
}

func (r *ReadinessHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	ua := req.UserAgent()

	_, span := r.trace.Start(ctx, "healthcheck", trace.WithAttributes(
		semconv.UserAgentName(ua),
	))
	defer span.End()

	switch r.available.Load() {
	case true:
		span.AddEvent("healthy")
		http.Error(w, "OK", http.StatusOK)
	case false:
		span.AddEvent("unhealthy")
		http.Error(w, "Shutting down", http.StatusServiceUnavailable)
	}
}

func produceTraces(ctx context.Context, tracer trace.Tracer) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Create a root span for a dummy operation
			ctx, span := tracer.Start(ctx, "dummy.operation")

			// Simulate some work with nested spans
			func() {
				_, childSpan := tracer.Start(ctx, "dummy.fetch_data",
					trace.WithAttributes(
						semconv.HTTPRequestMethodGet,
						semconv.HTTPResponseStatusCode(200),
					),
				)
				defer childSpan.End()

				childSpan.AddEvent("fetching data from database")
				time.Sleep(100 * time.Millisecond)
			}()

			func() {
				_, childSpan := tracer.Start(ctx, "dummy.process_data")
				defer childSpan.End()

				childSpan.AddEvent("processing data")
				time.Sleep(50 * time.Millisecond)
			}()

			func() {
				_, childSpan := tracer.Start(ctx, "dummy.save_result",
					trace.WithAttributes(
						semconv.DBSystemNamePostgreSQL,
						semconv.DBOperationName("insert"),
					),
				)
				defer childSpan.End()

				childSpan.AddEvent("saving result to database")
				time.Sleep(75 * time.Millisecond)
			}()

			span.AddEvent("operation completed successfully")
			span.End()

			slog.Info("Generated dummy trace")
		}
	}
}
