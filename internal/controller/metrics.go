package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	lastSuccessGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "capulus_backup_last_success_timestamp_seconds",
		Help: "Unix-Zeitpunkt des letzten erfolgreichen Backup-Laufs.",
	}, []string{"namespace", "name"})

	lastScheduleGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "capulus_backup_last_schedule_timestamp_seconds",
		Help: "Unix-Zeitpunkt des letzten gestarteten Backup-Laufs.",
	}, []string{"namespace", "name"})

	failedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "capulus_backup_failed",
		Help: "1, wenn der letzte Backup-Lauf fehlgeschlagen ist.",
	}, []string{"namespace", "name"})
)

func init() {
	metrics.Registry.MustRegister(lastSuccessGauge, lastScheduleGauge, failedGauge)
}
