package proxy

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// TracerProvider обёртка над OTel TracerProvider
type TracerProvider struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	logger   *zap.Logger
}

// NewTracerProvider создаёт OTel tracer provider из env-переменных
func NewTracerProvider(serviceName string, logger *zap.Logger) (*TracerProvider, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		logger.Info("OpenTelemetry disabled (no OTEL_EXPORTER_OTLP_ENDPOINT)")
		return nil, nil
	}

	opts := []otlptracehttp.Option{}

	// Insecure по умолчанию для dev/локальных collector'ов
	if os.Getenv("OTEL_INSECURE") == "true" || os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") == "true" {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(semconv.ServiceNameKey.String(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer := provider.Tracer(serviceName)

	logger.Info("OpenTelemetry initialized",
		zap.String("endpoint", endpoint),
	)

	return &TracerProvider{
		provider: provider,
		tracer:   tracer,
		logger:   logger,
	}, nil
}

// Shutdown корректно завершает провайдер
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp == nil || tp.provider == nil {
		return nil
	}
	tp.logger.Info("Shutting down OTel tracer provider")
	return tp.provider.Shutdown(ctx)
}

// Tracer возвращает tracer
func (tp *TracerProvider) Tracer() trace.Tracer {
	if tp == nil {
		return trace.NewNoopTracerProvider().Tracer("noop")
	}
	return tp.tracer
}
