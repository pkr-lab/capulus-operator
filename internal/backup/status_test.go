package backup

import (
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/pkr-lab/capulus-operator/api/v1alpha1"
)

func at(h int) *metav1.Time {
	t := metav1.NewTime(time.Date(2026, 10, 23, h, 0, 0, 0, time.UTC))
	return &t
}

func TestComputePhase(t *testing.T) {
	cases := []struct {
		name    string
		suspend bool
		cj      *batchv1.CronJob
		want    v1alpha1.BackupPhase
	}{
		{"suspendiert", true, &batchv1.CronJob{}, v1alpha1.BackupPhaseSuspended},
		{"kein CronJob", false, nil, v1alpha1.BackupPhasePending},
		{"nie gelaufen", false, &batchv1.CronJob{}, v1alpha1.BackupPhasePending},
		{"läuft", false, &batchv1.CronJob{Status: batchv1.CronJobStatus{
			LastScheduleTime: at(2),
			Active:           []corev1.ObjectReference{{Name: "j"}},
		}}, v1alpha1.BackupPhaseRunning},
		{"erfolgreich", false, &batchv1.CronJob{Status: batchv1.CronJobStatus{
			LastScheduleTime: at(2), LastSuccessfulTime: at(3),
		}}, v1alpha1.BackupPhaseSucceeded},
		{"nur manueller Erfolg", false, &batchv1.CronJob{Status: batchv1.CronJobStatus{
			LastSuccessfulTime: at(3),
		}}, v1alpha1.BackupPhaseSucceeded},
		{"fehlgeschlagen nach früherem Erfolg", false, &batchv1.CronJob{Status: batchv1.CronJobStatus{
			LastScheduleTime: at(5), LastSuccessfulTime: at(3),
		}}, v1alpha1.BackupPhaseFailed},
		{"fehlgeschlagen ohne Erfolg", false, &batchv1.CronJob{Status: batchv1.CronJobStatus{
			LastScheduleTime: at(5),
		}}, v1alpha1.BackupPhaseFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &v1alpha1.Backup{Spec: v1alpha1.BackupSpec{Suspend: c.suspend}}
			if got := ComputePhase(b, c.cj); got != c.want {
				t.Fatalf("erwartet %s, erhalten %s", c.want, got)
			}
		})
	}
}

func TestHealthCondition(t *testing.T) {
	cases := map[v1alpha1.BackupPhase]metav1.ConditionStatus{
		v1alpha1.BackupPhaseSucceeded: metav1.ConditionTrue,
		v1alpha1.BackupPhaseFailed:    metav1.ConditionFalse,
		v1alpha1.BackupPhaseRunning:   metav1.ConditionUnknown,
		v1alpha1.BackupPhasePending:   metav1.ConditionUnknown,
		v1alpha1.BackupPhaseSuspended: metav1.ConditionUnknown,
	}
	for phase, want := range cases {
		if got := HealthCondition(phase, 1).Status; got != want {
			t.Fatalf("%s: erwartet %s, erhalten %s", phase, want, got)
		}
	}
}
