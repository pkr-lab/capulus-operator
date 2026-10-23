package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/pkr-lab/capulus-operator/api/v1alpha1"
	"github.com/pkr-lab/capulus-operator/internal/backup"
	"github.com/pkr-lab/capulus-operator/internal/controller"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr   string
		probeAddr     string
		leaderElect   bool
		leaderElectNS string
		defaults      backup.Defaults
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Adresse für /metrics")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Adresse für /healthz und /readyz")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Leader Election aktivieren")
	flag.StringVar(&leaderElectNS, "leader-election-namespace", "", "Namespace des Leader-Lease")
	flag.StringVar(&defaults.ResticImage, "restic-image", "restic/restic:0.18.1", "Standard-Image für restic")
	flag.StringVar(&defaults.SQLiteImage, "sqlite-image", "alpine:3.24", "Standard-Image für SQLite-Dumps")
	flag.StringVar(&defaults.PostgresImage, "postgres-image", "postgres:17-alpine", "Standard-Image für pg_dump")
	zapOpts := zap.Options{}
	zapOpts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))
	setupLog := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          leaderElect,
		LeaderElectionID:        "capulus-operator.capulus.homeserver",
		LeaderElectionNamespace: leaderElectNS,
	})
	if err != nil {
		setupLog.Error(err, "Manager konnte nicht erstellt werden")
		os.Exit(1)
	}

	if err := (&controller.BackupReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Defaults: defaults,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Backup-Controller konnte nicht registriert werden")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "healthz fehlgeschlagen")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "readyz fehlgeschlagen")
		os.Exit(1)
	}

	setupLog.Info("Operator startet")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Manager beendet")
		os.Exit(1)
	}
}
