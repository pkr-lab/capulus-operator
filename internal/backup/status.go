package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/pkr-lab/capulus-operator/api/v1alpha1"
)

func SpecHash(cj *batchv1.CronJob) (string, error) {
	raw, err := json.Marshal(cj.Spec)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func ComputePhase(b *v1alpha1.Backup, cj *batchv1.CronJob) v1alpha1.BackupPhase {
	if b.Spec.Suspend {
		return v1alpha1.BackupPhaseSuspended
	}
	if cj == nil {
		return v1alpha1.BackupPhasePending
	}
	if len(cj.Status.Active) > 0 {
		return v1alpha1.BackupPhaseRunning
	}
	scheduled, succeeded := cj.Status.LastScheduleTime, cj.Status.LastSuccessfulTime
	switch {
	case scheduled == nil && succeeded == nil:
		return v1alpha1.BackupPhasePending
	case succeeded != nil && (scheduled == nil || !succeeded.Before(scheduled)):
		return v1alpha1.BackupPhaseSucceeded
	default:
		return v1alpha1.BackupPhaseFailed
	}
}

func ApplyStatus(b *v1alpha1.Backup, cj *batchv1.CronJob) {
	b.Status.ObservedGeneration = b.Generation
	b.Status.Phase = ComputePhase(b, cj)
	if cj == nil {
		b.Status.CronJobName = ""
		b.Status.LastScheduleTime = nil
		b.Status.LastSuccessfulTime = nil
		return
	}
	b.Status.CronJobName = cj.Name
	b.Status.LastScheduleTime = cj.Status.LastScheduleTime.DeepCopy()
	b.Status.LastSuccessfulTime = cj.Status.LastSuccessfulTime.DeepCopy()
}

func HealthCondition(phase v1alpha1.BackupPhase, generation int64) metav1.Condition {
	c := metav1.Condition{
		Type:               "Healthy",
		ObservedGeneration: generation,
	}
	switch phase {
	case v1alpha1.BackupPhaseSucceeded:
		c.Status, c.Reason, c.Message = metav1.ConditionTrue, "LastRunSucceeded", "letzter Lauf erfolgreich"
	case v1alpha1.BackupPhaseFailed:
		c.Status, c.Reason, c.Message = metav1.ConditionFalse, "LastRunFailed", "letzter Lauf fehlgeschlagen"
	case v1alpha1.BackupPhaseSuspended:
		c.Status, c.Reason, c.Message = metav1.ConditionUnknown, "Suspended", "Backup ist pausiert"
	default:
		c.Status, c.Reason, c.Message = metav1.ConditionUnknown, string(phase), "noch kein abgeschlossener Lauf"
	}
	return c
}
