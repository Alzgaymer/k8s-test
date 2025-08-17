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

	h, unaliveServer := NewRoutes()
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
}

func NewRoutes() (http.Handler, func()) {
	mux := http.NewServeMux()

	rh := NewReadinessHandler()

	mux.Handle("GET /health", rh)

	return mux, rh.MakeUnavailable
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
}

func NewReadinessHandler() *ReadinessHandler {
	available := atomic.Bool{}
	available.Store(true)
	return &ReadinessHandler{
		available: &available,
	}
}

func (r *ReadinessHandler) MakeUnavailable() {
	r.available.Store(false)
}

func (r *ReadinessHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch r.available.Load() {
	case true:
		http.Error(w, "OK", http.StatusOK)
	case false:
		http.Error(w, "Shutting down", http.StatusServiceUnavailable)
	}
}
