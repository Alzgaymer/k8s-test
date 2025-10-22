package main

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

func newTracer(ctx context.Context) (cleanup func(context.Context) error) {
	version := fmt.Sprintf("%s@%s", Commit, Version)

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("server"),
			semconv.ServiceVersion(version),
			semconv.DeploymentEnvironmentName("dev"),
		),
	)
	if err != nil {
		panic(err)
	}

	otel.SetErrorHandler(&SlogErrorHandler{slog.Default()})

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithInsecure())
	if err != nil {
		panic(err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Set as global tracer provider
	otel.SetTracerProvider(tp)

	slog.InfoContext(ctx, "otel tracer is configured")

	return tp.Shutdown
}

type SlogErrorHandler struct {
	log *slog.Logger
}

func (s *SlogErrorHandler) Handle(err error) {
	s.log.Error("Error in otel instrumentation", "err", err)
}
