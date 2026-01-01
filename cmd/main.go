/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	autoscalingv1alpha1 "github.com/sarabala1979/SmartHPA/api/v1alpha1"
	"github.com/sarabala1979/SmartHPA/internal/controller"
	"github.com/sarabala1979/SmartHPA/internal/mlserver"
	"github.com/sarabala1979/SmartHPA/internal/scheduler"
	"github.com/sarabala1979/SmartHPA/internal/server"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(autoscalingv2.AddToScheme(scheme))
	utilruntime.Must(autoscalingv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	// Mode flags
	var runController bool
	var runServer bool
	var serverAddr string
	var runML bool
	var mlAddr string
	var mlPrometheusURL string
	var mlModulePath string

	// Controller flags
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool

	flag.BoolVar(&runController, "controller", false, "Run the SmartHPA controller")
	flag.BoolVar(&runServer, "server", false, "Run the REST API server with embedded UI")
	flag.StringVar(&serverAddr, "server-addr", ":8090", "Address for the REST API server (used with --server)")
	flag.BoolVar(&runML, "ml", false, "Run the ML trigger generation service")
	flag.StringVar(&mlAddr, "ml-addr", ":8091", "Address for the ML service (used with --ml)")
	flag.StringVar(&mlPrometheusURL, "ml-prometheus-url", "http://localhost:9090", "Prometheus URL for ML service")
	flag.StringVar(&mlModulePath, "ml-module-path", "./ml", "Path to ML Python module")

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// If no flags are specified, default to controller mode for backward compatibility
	if !runController && !runServer && !runML {
		runController = true
	}

	// Log which modes are enabled
	modes := []string{}
	if runController {
		modes = append(modes, "controller")
	}
	if runServer {
		modes = append(modes, "server")
	}
	if runML {
		modes = append(modes, "ml")
	}
	setupLog.Info("Starting SmartHPA", "modes", modes)

	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		setupLog.Info("Received shutdown signal")
		cancel()
	}()

	if runServer {
		go func() {
			if err := runAPIServer(ctx, serverAddr); err != nil {
				setupLog.Error(err, "REST API server error")
			}
		}()
	}

	if runML {
		go func() {
			if err := runMLServer(ctx, mlAddr, mlPrometheusURL, mlModulePath); err != nil {
				setupLog.Error(err, "ML server error")
			}
		}()
	}

	if runController {
		if err := runControllerManager(ctx, metricsAddr, probeAddr, enableLeaderElection, secureMetrics, enableHTTP2); err != nil {
			setupLog.Error(err, "problem running controller manager")
			os.Exit(1)
		}
	} else {
		// If server-only or ML-only mode, wait for context cancellation
		setupLog.Info("Running in server/ML only mode, waiting for shutdown signal")
		<-ctx.Done()
	}
}

// runAPIServer starts the REST API server with embedded UI
func runAPIServer(ctx context.Context, addr string) error {
	setupLog.Info("Starting REST API server", "addr", addr)

	// Create K8s client
	cfg := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create K8s client: %w", err)
	}

	// Create and start server
	srv := server.NewServer(k8sClient, addr)
	return srv.Start(ctx)
}

// runMLServer starts the ML trigger generation service
func runMLServer(ctx context.Context, addr, prometheusURL, modulePath string) error {
	setupLog.Info("Starting ML server", "addr", addr, "prometheus", prometheusURL)

	config := mlserver.Config{
		Addr:          addr,
		PrometheusURL: prometheusURL,
		MLModulePath:  modulePath,
	}

	srv := mlserver.NewServer(config)
	return srv.Start(ctx)
}

// runControllerManager runs the SmartHPA controller
func runControllerManager(ctx context.Context, metricsAddr, probeAddr string, enableLeaderElection, secureMetrics, enableHTTP2 bool) error {
	var tlsOpts []func(*tls.Config)

	// Use buffered queue for better throughput and to prevent blocking
	queue := scheduler.NewSchedulerQueue()

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		// TODO(user): TLSOpts is used to allow configuring the TLS config used for the server. If certificates are
		// not provided, self-signed certificates will be generated by default. This option is not recommended for
		// production environments as self-signed certificates do not offer the same level of trust and security
		// as certificates issued by a trusted Certificate Authority (CA). The primary risk is potentially allowing
		// unauthorized access to sensitive metrics data. Consider replacing with CertDir, CertName, and KeyName
		// to provide certificates, ensuring the server communicates using trusted and secure certificates.
		TLSOpts: tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9d81a9d0.sarabala.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	// Create scheduler first so we can pass it to the controller
	hpaScheduler := scheduler.NewScheduler(mgr.GetClient(), queue)

	// Setup controller with scheduler reference for cleanup coordination
	if err = (&controller.SmartHorizontalPodAutoscalerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, queue, hpaScheduler); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	// Register scheduler with manager for proper lifecycle management
	if err := mgr.Add(hpaScheduler); err != nil {
		return fmt.Errorf("unable to add scheduler to manager: %w", err)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}
