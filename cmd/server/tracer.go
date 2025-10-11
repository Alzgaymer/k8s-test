package main

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
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

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(), // Use WithTLSCredentials in production
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.NeverSample()),
	)

	// Set as global tracer provider
	otel.SetTracerProvider(tp)

	slog.InfoContext(ctx, "otel tracer is configured")

	return tp.Shutdown
}
