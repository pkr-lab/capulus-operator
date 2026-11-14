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
	cases := map[v1alpha1.BackupPhase]struct {
		status metav1.ConditionStatus
		reason string
	}{
		v1alpha1.BackupPhaseSucceeded: {metav1.ConditionTrue, ReasonLastRunSucceeded},
		v1alpha1.BackupPhaseFailed:    {metav1.ConditionFalse, ReasonLastRunFailed},
		v1alpha1.BackupPhaseRunning:   {metav1.ConditionUnknown, string(v1alpha1.BackupPhaseRunning)},
		v1alpha1.BackupPhasePending:   {metav1.ConditionUnknown, string(v1alpha1.BackupPhasePending)},
		v1alpha1.BackupPhaseSuspended: {metav1.ConditionUnknown, ReasonSuspended},
	}
	for phase, want := range cases {
		got := HealthCondition(phase, 7)
		if got.Type != ConditionHealthy || got.Status != want.status || got.Reason != want.reason || got.ObservedGeneration != 7 {
			t.Fatalf("%s: erwartet %s/%s, erhalten %+v", phase, want.status, want.reason, got)
		}
	}
}

func TestApplyStatus(t *testing.T) {
	b := &v1alpha1.Backup{}
	b.Generation = 4
	cj := &batchv1.CronJob{Status: batchv1.CronJobStatus{LastScheduleTime: at(2), LastSuccessfulTime: at(3)}}
	cj.Name = "vw"
	ApplyStatus(b, cj)
	if b.Status.ObservedGeneration != 4 || b.Status.CronJobName != "vw" || b.Status.Phase != v1alpha1.BackupPhaseSucceeded {
		t.Fatalf("Status unerwartet: %+v", b.Status)
	}
	if !b.Status.LastScheduleTime.Equal(at(2)) || !b.Status.LastSuccessfulTime.Equal(at(3)) {
		t.Fatalf("Zeitstempel unerwartet: %+v", b.Status)
	}
	if b.Status.LastSuccessfulTime == cj.Status.LastSuccessfulTime {
		t.Fatal("Zeitstempel nicht kopiert")
	}

	ApplyStatus(b, nil)
	if b.Status.CronJobName != "" || b.Status.LastScheduleTime != nil || b.Status.LastSuccessfulTime != nil || b.Status.Phase != v1alpha1.BackupPhasePending {
		t.Fatalf("Status ohne CronJob unerwartet: %+v", b.Status)
	}
}
