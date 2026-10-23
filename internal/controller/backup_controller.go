package controller

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/pkr-lab/capulus-operator/api/v1alpha1"
	"github.com/pkr-lab/capulus-operator/internal/backup"
)

const specHashAnnotation = "capulus.homeserver/spec-hash"

type BackupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Defaults backup.Defaults
}

// +kubebuilder:rbac:groups=capulus.homeserver,resources=backups,verbs=get;list;watch
// +kubebuilder:rbac:groups=capulus.homeserver,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var b v1alpha1.Backup
	if err := r.Get(ctx, req.NamespacedName, &b); err != nil {
		if apierrors.IsNotFound(err) {
			forgetMetrics(req.Namespace, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !b.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	orig := b.DeepCopy()

	cj, err := r.syncCronJob(ctx, &b)
	if err != nil {
		logger.Error(err, "CronJob konnte nicht abgeglichen werden")
		meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "ReconcileFailed",
			Message:            err.Error(),
			ObservedGeneration: b.Generation,
		})
		b.Status.ObservedGeneration = b.Generation
		if statusErr := r.Status().Patch(ctx, &b, client.MergeFrom(orig)); statusErr != nil {
			logger.Error(statusErr, "Status konnte nicht geschrieben werden")
		}
		return ctrl.Result{}, err
	}

	backup.ApplyStatus(&b, cj)
	meta.SetStatusCondition(&b.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "CronJobSynced",
		Message:            fmt.Sprintf("CronJob %s abgeglichen", cj.Name),
		ObservedGeneration: b.Generation,
	})
	meta.SetStatusCondition(&b.Status.Conditions, backup.HealthCondition(b.Status.Phase, b.Generation))
	recordMetrics(&b)

	if err := r.Status().Patch(ctx, &b, client.MergeFrom(orig)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *BackupReconciler) syncCronJob(ctx context.Context, b *v1alpha1.Backup) (*batchv1.CronJob, error) {
	desired, err := backup.BuildCronJob(b, r.Defaults)
	if err != nil {
		return nil, err
	}
	hash, err := backup.SpecHash(desired)
	if err != nil {
		return nil, err
	}
	desired.Annotations = map[string]string{specHashAnnotation: hash}
	if err := controllerutil.SetControllerReference(b, desired, r.Scheme); err != nil {
		return nil, err
	}

	var existing batchv1.CronJob
	err = r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return nil, err
		}
		return desired, nil
	}
	if err != nil {
		return nil, err
	}
	if !metav1.IsControlledBy(&existing, b) {
		return nil, fmt.Errorf("CronJob %s gehört nicht zu diesem Backup", existing.Name)
	}
	if existing.Annotations[specHashAnnotation] == hash {
		return &existing, nil
	}

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations[specHashAnnotation] = hash
	if err := r.Update(ctx, &existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Backup{}).
		Owns(&batchv1.CronJob{}).
		Complete(r)
}

func recordMetrics(b *v1alpha1.Backup) {
	l := []string{b.Namespace, b.Name}
	if b.Status.LastSuccessfulTime != nil {
		lastSuccessGauge.WithLabelValues(l...).Set(float64(b.Status.LastSuccessfulTime.Unix()))
	}
	if b.Status.LastScheduleTime != nil {
		lastScheduleGauge.WithLabelValues(l...).Set(float64(b.Status.LastScheduleTime.Unix()))
	}
	failed := 0.0
	if b.Status.Phase == v1alpha1.BackupPhaseFailed {
		failed = 1
	}
	failedGauge.WithLabelValues(l...).Set(failed)
	suspended := 0.0
	if b.Spec.Suspend {
		suspended = 1
	}
	suspendedGauge.WithLabelValues(l...).Set(suspended)
}

func forgetMetrics(namespace, name string) {
	l := []string{namespace, name}
	lastSuccessGauge.DeleteLabelValues(l...)
	lastScheduleGauge.DeleteLabelValues(l...)
	failedGauge.DeleteLabelValues(l...)
	suspendedGauge.DeleteLabelValues(l...)
}
