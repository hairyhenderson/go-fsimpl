package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

func initTracing(ctx context.Context) (func(context.Context) error, error) {
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlptracegrpc.New: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithProcessRuntimeDescription(),
		resource.WithAttributes(semconv.ServiceNameKey.String("fscli")),
	)
	if err != nil {
		return nil, fmt.Errorf("resource.New: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())

	otel.SetErrorHandler(otelErrHandler(func(err error) {
		slog.Error("OTel error", slog.Any("err", err))
	}))

	shutdown := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if err := tp.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown trace provider: %w", err)
		}

		return nil
	}

	return shutdown, nil
}

type otelErrHandler func(err error)

func (o otelErrHandler) Handle(err error) {
	o(err)
}
