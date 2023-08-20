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
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	serviceName  = "otelo"
	collectorURL = "localhost:4317"
)

func main() {
	ctx := context.Background()

	shutdown := initTracer()
	defer shutdown(ctx)

	shutdown2 := setupMetrics(ctx)
	defer shutdown2(ctx)

	counter, err := otel.GetMeterProvider().
		Meter(
			"instrumentation/package/name",
			metric.WithInstrumentationVersion("0.0.1"),
		).
		Int64Counter(
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
				fmt.Println("end")
				return
			case <-time.After(1 * time.Second):
				fmt.Println("sending count", time.Now())
				_, span := otel.Tracer("hallo").Start(ctx, "Run")
				span.SetAttributes(attribute.Int("request.n", 10))

				defer span.End()

				counter.Add(
					ctx,
					1,
					// labels/tags
					metric.WithAttributes(
						attribute.Key("key").String("component"),
					),
				)
			}
		}
	}()

	log.Println("listening to port *:8000")
	http.Handle("/metrics", promhttp.Handler())
	panic(http.ListenAndServe(":8000", nil))
}

func initTracer() func(context.Context) error {
	exporter, err := otlptrace.New(
		context.Background(),
		otlptracegrpc.NewClient(
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithEndpoint(collectorURL),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		log.Println("Could not set resources: ", err)
	}

	// Trace
	otel.SetTracerProvider(
		sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			// set the sampling rate based on the parent span to 60%
			//trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(0.6))),
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resources),
		),
	)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{}, // W3C Trace Context format; https://www.w3.org/TR/trace-context/
		),
	)

	return exporter.Shutdown
}

func setupMetrics(ctx context.Context) func(context.Context) error {

	// Push-based periodic OTLP exporter
	exporter, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithEndpoint(collectorURL),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		panic(err)
	}

	// Pull-based Prometheus exporter
	prometheusExporter, err := prometheus.New()
	if err != nil {
		panic(err)
	}

	// labels/tags/resources that are common to all metrics.
	resource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		attribute.String("some-attribute", "some-value"),
	)

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(prometheusExporter),
		sdkmetric.WithResource(resource),
		sdkmetric.WithReader(
			// collects and exports metric data every 10 seconds.
			sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(10*time.Second)),
		),
	)

	// Metrics
	otel.SetMeterProvider(
		provider,
	)
	meter := provider.Meter("github.com/hello")
	counter, err := meter.Int64Counter("foo")
	if err != nil {
		panic(err)
	}
	counter.Add(ctx, 5)

	return prometheusExporter.Shutdown
}
