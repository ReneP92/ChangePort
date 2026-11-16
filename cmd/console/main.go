// Command console serves the Changeport console: the API of
// api/openapi.yaml behind Clerk sign-in, and the web app.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ReneP92/changeport/api/v1alpha1"
	"github.com/ReneP92/changeport/internal/alerts"
	"github.com/ReneP92/changeport/internal/cdc"
	"github.com/ReneP92/changeport/internal/clusters"
	"github.com/ReneP92/changeport/internal/console"
	"github.com/ReneP92/changeport/internal/console/ui"
	"github.com/ReneP92/changeport/internal/events"
	"github.com/ReneP92/changeport/internal/mail"
	"github.com/ReneP92/changeport/internal/metrics"
	"github.com/ReneP92/changeport/internal/provisioning"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	stop()
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("console", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "address to serve on")
	metricsURL := fs.String("metrics-url", envOr("CHANGEPORT_METRICS_URL", "http://victoria-metrics.metrics.svc:8428"),
		"address of the metrics store (VictoriaMetrics)")
	kubeContext := fs.String("context", "", "kubeconfig context to use (default: the current context, or the pod's service account in a cluster)")
	allowPrivateDatabases := fs.Bool("allow-private-databases", false,
		"let Postgres sources' databases be on private networks, as on kind, where a Postgres stands in for customers'")
	eventsURL := fs.String("events-url", envOr("CHANGEPORT_EVENTS_URL", provisioning.EventsURL),
		"address of VictoriaLogs, which holds the change timeline")
	alertmanagerURL := fs.String("alertmanager-url", envOr("CHANGEPORT_ALERTMANAGER_URL", "http://alertmanager.metrics.svc:9093"),
		"address of Alertmanager, which holds the customers' active alerts")
	allowPrivateReceivers := fs.Bool("allow-private-receivers", false,
		"let alert webhooks be on private networks and plain HTTP, as on kind, where a test webhook stands in for customers'")
	metricsListen := fs.String("metrics-listen", ":9090",
		"address to serve the console's own metrics on, the health levels, for VictoriaMetrics; only inside the cluster")
	healthInterval := fs.Duration("health-interval", time.Minute,
		"how often the console diagnoses every ready customer, for the health histories")
	diagnosisWindow := fs.Duration("diagnosis-window", metrics.DefaultWindow,
		"how far back the diagnoses look (docs/diagnosis.md); shorter on kind, so that the end-to-end test needs minutes")
	maxEnvironments := fs.Int("max-environments", console.DefaultMaxEnvironments,
		"the most environments, each a cluster, an organization's admins may have; operators add beyond it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *healthInterval <= 0 || *diagnosisWindow <= 0 {
		return errors.New("the health interval and the diagnosis window must be longer than nothing")
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// The customer watch's informers log through controller-runtime, which
	// otherwise prints a stack trace at start instead.
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: *kubeContext},
	).ClientConfig()
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return err
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	// client-go's default of 5 requests a second, in bursts of 10, would
	// queue a busy console's requests behind each other for seconds.
	config.QPS, config.Burst = 50, 100
	kube, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("connect to the cluster: %w", err)
	}
	customers, err := console.WatchCustomers(ctx, config, scheme)
	if err != nil {
		return err
	}

	metricsClient, err := metrics.NewClient(*metricsURL)
	if err != nil {
		return err
	}
	timeline, err := events.NewClient(*eventsURL)
	if err != nil {
		return err
	}
	recorder := events.NewRecorder(timeline, logger, events.RecorderOptions{})
	recorded := make(chan struct{})
	go func() { recorder.Run(ctx); close(recorded) }()
	// The last changes users made get written before the console exits.
	defer func() { <-recorded }()
	kafka := clusters.NewPool(console.KubeSecrets{Client: kube}, logger)
	defer kafka.Close()

	// Email about trials (internal/mail), off without CHANGEPORT_MAIL.
	mailer, err := mail.New(ctx, os.Getenv("CHANGEPORT_MAIL"), os.Getenv("CHANGEPORT_MAIL_FROM"), logger)
	if err != nil {
		return err
	}

	docsURL := os.Getenv("CHANGEPORT_DOCS_URL")
	if docsURL != "" {
		if err := alerts.ValidateDocsURL(docsURL); err != nil {
			return fmt.Errorf("CHANGEPORT_DOCS_URL: %w", err)
		}
	}
	server, err := console.New(console.Config{
		PublishableKey:      os.Getenv("CLERK_PUBLISHABLE_KEY"),
		SecretKey:           os.Getenv("CLERK_SECRET_KEY"),
		StaffOrganizationID: os.Getenv("CHANGEPORT_STAFF_ORG_ID"),
		Origins:             splitList(os.Getenv("CHANGEPORT_CONSOLE_ORIGINS")),
		OperatorEmails:      splitList(os.Getenv("CHANGEPORT_OPERATOR_EMAILS")),
		SupportEmail:        os.Getenv("CHANGEPORT_SUPPORT_EMAIL"),
		DocsURL:             docsURL,
		MaxEnvironments:     *maxEnvironments,
		DiagnosisWindow:     *diagnosisWindow,
	}, console.Options{
		Mail:          mailer,
		Customers:     console.KubeCustomers{Client: kube, Cache: customers},
		TrialRequests: console.KubeTrialRequests{Client: kube},
		Clusters:      console.KubeClusters{Client: kube, Kafka: kafka},
		Metrics:       metricsClient,
		Sources:       console.KubeSources{Client: kube},
		Databases:     cdc.Checker{Guard: cdc.Guard{AllowPrivate: *allowPrivateDatabases}},
		Alerts: console.AlertmanagerAlerts{
			URL: *alertmanagerURL, HTTP: &http.Client{Timeout: 10 * time.Second},
		},
		AlertSettings: console.KubeAlertSettings{Client: kube},
		Events:        timeline,
		Messages:      console.KubeMessages{Kafka: kafka},
		Recorder:      recorder,
		Receivers:     console.Receivers{AllowPrivate: *allowPrivateReceivers},
		// The addresses the platform's NAT gateways give its connections
		// to the internet (Terraform's nat_public_ips).
		EgressAddresses: splitList(os.Getenv("CHANGEPORT_EGRESS_IPS")),
		UI:              ui.FS(),
		Logger:          logger,
	})
	if err != nil {
		return err
	}
	go server.WarmUp(ctx)
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	// Its own metrics, apart from the API, which the internet reaches.
	registry := prometheus.NewRegistry()
	registry.MustRegister(server.HealthCollector())
	metricsServer := &http.Server{
		Addr:              *metricsListen,
		Handler:           promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go server.WatchHealth(ctx, *healthInterval)
	served := make(chan error, 2)
	go func() { served <- httpServer.ListenAndServe() }()
	go func() { served <- metricsServer.ListenAndServe() }()
	logger.Info("console serving", "address", *listen, "metrics", *metricsListen)
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(httpServer.Shutdown(shutdown), metricsServer.Shutdown(shutdown))
}

// envOr returns the environment variable name, or fallback when it is unset.
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// splitList splits a comma-separated list, dropping empty items.
func splitList(s string) []string {
	var items []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
