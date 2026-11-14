package controller

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/pkr-lab/capulus-operator/api/v1alpha1"
)

func TestRecordMetrics(t *testing.T) {
	scheduled := metav1.NewTime(time.Unix(1_700_000_000, 0))
	succeeded := metav1.NewTime(time.Unix(1_700_000_120, 0))
	b := &v1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{Namespace: "metrics", Name: "record"},
		Spec:       v1alpha1.BackupSpec{Suspend: true},
		Status: v1alpha1.BackupStatus{
			Phase:              v1alpha1.BackupPhaseFailed,
			LastScheduleTime:   &scheduled,
			LastSuccessfulTime: &succeeded,
		},
	}
	t.Cleanup(func() { forgetMetrics(b.Namespace, b.Name) })

	recordMetrics(b)

	l := []string{b.Namespace, b.Name}
	checks := map[string]struct{ got, want float64 }{
		"last_success":  {testutil.ToFloat64(lastSuccessGauge.WithLabelValues(l...)), 1_700_000_120},
		"last_schedule": {testutil.ToFloat64(lastScheduleGauge.WithLabelValues(l...)), 1_700_000_000},
		"failed":        {testutil.ToFloat64(failedGauge.WithLabelValues(l...)), 1},
		"suspended":     {testutil.ToFloat64(suspendedGauge.WithLabelValues(l...)), 1},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: erwartet %v, erhalten %v", name, c.want, c.got)
		}
	}

	b.Spec.Suspend = false
	b.Status.Phase = v1alpha1.BackupPhaseSucceeded
	recordMetrics(b)
	if got := testutil.ToFloat64(failedGauge.WithLabelValues(l...)); got != 0 {
		t.Errorf("failed nach Erfolg: %v", got)
	}
	if got := testutil.ToFloat64(suspendedGauge.WithLabelValues(l...)); got != 0 {
		t.Errorf("suspended nach Fortsetzen: %v", got)
	}
}

func TestForgetMetrics(t *testing.T) {
	b := &v1alpha1.Backup{ObjectMeta: metav1.ObjectMeta{Namespace: "metrics", Name: "forget"}}
	recordMetrics(b)
	gauges := map[string]*prometheus.GaugeVec{"failed": failedGauge, "suspended": suspendedGauge}
	before := map[string]int{}
	for name, g := range gauges {
		before[name] = testutil.CollectAndCount(g)
	}

	forgetMetrics(b.Namespace, b.Name)

	for name, g := range gauges {
		if after := testutil.CollectAndCount(g); after != before[name]-1 {
			t.Errorf("%s: Serie nicht entfernt (%d -> %d)", name, before[name], after)
		}
	}
}
