package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"

	"go.opentelemetry.io/otel/sdk/resource"
)

var (
	serviceName  = "otelo"
	collectorURL = "localhost:4317"
)

func main() {
	ctx := context.Background()

	shutdown := setupMetrics(ctx)
	defer shutdown(ctx)

	meter := otel.GetMeterProvider().Meter(
		"instrumentation/package/name",             // This will appear as `otel_scope_name`.
		metric.WithInstrumentationVersion("0.0.1"), // This will appear as `otel_scope_version`.
	)

	counter, err := meter.Int64Counter(
		"add_count",
		metric.WithDescription("how many times add function has been called."),
	)
	if err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		defer stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
				fmt.Println("inc", time.Now())

				counter.Add(ctx, 1, metric.WithAttributes(
					attribute.Key("key").String("component"),
				))
			}
		}
	}()

	log.Println("listening to port *:8000")
	http.Handle("/metrics", promhttp.Handler())
	panic(http.ListenAndServe(":8000", nil))
}

func setupMetrics(ctx context.Context) func(context.Context) error {
	// Pull-based Prometheus exporter
	exporter, err := prometheus.New()
	if err != nil {
		panic(err)
	}

	// labels/tags/resources that are common to all metrics.
	resource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		attribute.String("some-attribute", "some-value"),
	)

	// Metrics
	otel.SetMeterProvider(
		sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(exporter),
			sdkmetric.WithResource(resource),
		),
	)

	return exporter.Shutdown
}
