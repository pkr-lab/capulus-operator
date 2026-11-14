package controller

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/pkr-lab/capulus-operator/api/v1alpha1"
	"github.com/pkr-lab/capulus-operator/internal/backup"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func testBackup() *v1alpha1.Backup {
	return &v1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "vw", Namespace: "apps", UID: "uid-1", Generation: 1},
		Spec: v1alpha1.BackupSpec{
			Schedule: "10 2 * * *",
			Repository: v1alpha1.BackupRepository{
				PVCName:           "vw-restic",
				PasswordSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "vw-restic"}, Key: "password"},
			},
			Sources: []v1alpha1.BackupSource{{Name: "data", Type: v1alpha1.SourceTypeFiles, PVCName: "vw-data"}},
		},
	}
}

func newReconciler(t *testing.T, objs ...client.Object) (*BackupReconciler, client.Client) {
	t.Helper()
	s := testScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.Backup{}).
		Build()
	return &BackupReconciler{
		Client:   c,
		Scheme:   s,
		Defaults: backup.Defaults{ResticImage: "restic/restic:test", SQLiteImage: "alpine:test", PostgresImage: "postgres:test"},
	}, c
}

var backupKey = types.NamespacedName{Namespace: "apps", Name: "vw"}

func getBackup(t *testing.T, c client.Client) v1alpha1.Backup {
	t.Helper()
	var b v1alpha1.Backup
	if err := c.Get(context.Background(), backupKey, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func getCronJob(t *testing.T, c client.Client) batchv1.CronJob {
	t.Helper()
	var cj batchv1.CronJob
	if err := c.Get(context.Background(), backupKey, &cj); err != nil {
		t.Fatal(err)
	}
	return cj
}

func envValue(c corev1.Container, name string) string {
	for _, e := range c.Env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

func reconcile(t *testing.T, r *BackupReconciler) error {
	t.Helper()
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: backupKey})
	return err
}

func TestReconcileCreatesCronJob(t *testing.T) {
	r, c := newReconciler(t, testBackup())
	if err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}

	cj := getCronJob(t, c)
	if !metav1.IsControlledBy(&cj, testBackup()) {
		t.Fatalf("CronJob hat keinen Owner: %+v", cj.OwnerReferences)
	}
	if cj.Annotations[specHashAnnotation] == "" {
		t.Fatal("Hash-Annotation fehlt")
	}

	b := getBackup(t, c)
	if b.Status.Phase != v1alpha1.BackupPhasePending || b.Status.CronJobName != "vw" {
		t.Fatalf("Status: %+v", b.Status)
	}
	if !meta.IsStatusConditionTrue(b.Status.Conditions, backup.ConditionReady) {
		t.Fatalf("Ready nicht gesetzt: %+v", b.Status.Conditions)
	}
}

func TestReconcileUpdatesOnSpecChange(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*v1alpha1.Backup)
		check  func(*testing.T, batchv1.CronJob)
	}{
		{
			name:   "Zeitplan",
			mutate: func(b *v1alpha1.Backup) { b.Spec.Schedule = "0 4 * * *" },
			check: func(t *testing.T, cj batchv1.CronJob) {
				if cj.Spec.Schedule != "0 4 * * *" {
					t.Fatalf("Zeitplan nicht aktualisiert: %s", cj.Spec.Schedule)
				}
			},
		},
		{
			name:   "Retention",
			mutate: func(b *v1alpha1.Backup) { b.Spec.Retention.KeepDaily = 14 },
			check: func(t *testing.T, cj batchv1.CronJob) {
				if got := envValue(cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0], "KEEP_DAILY"); got != "14" {
					t.Fatalf("KEEP_DAILY: %q", got)
				}
			},
		},
		{
			name:   "Suspend",
			mutate: func(b *v1alpha1.Backup) { b.Spec.Suspend = true },
			check: func(t *testing.T, cj batchv1.CronJob) {
				if cj.Spec.Suspend == nil || !*cj.Spec.Suspend {
					t.Fatal("Suspend nicht übernommen")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, c := newReconciler(t, testBackup())
			if err := reconcile(t, r); err != nil {
				t.Fatal(err)
			}
			b := getBackup(t, c)
			tc.mutate(&b)
			if err := c.Update(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			if err := reconcile(t, r); err != nil {
				t.Fatal(err)
			}
			tc.check(t, getCronJob(t, c))
		})
	}
}

func TestReconcileWithoutChangeKeepsCronJob(t *testing.T) {
	r, c := newReconciler(t, testBackup())
	if err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	before := getCronJob(t, c).ResourceVersion
	if err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	if after := getCronJob(t, c).ResourceVersion; after != before {
		t.Fatalf("CronJob ohne Spec-Änderung aktualisiert: %s -> %s", before, after)
	}
}

func TestReconcileRestoresLabelsOnUpdate(t *testing.T) {
	r, c := newReconciler(t, testBackup())
	if err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	cj := getCronJob(t, c)
	cj.Labels = map[string]string{"fremd": "x"}
	if err := c.Update(context.Background(), &cj); err != nil {
		t.Fatal(err)
	}
	b := getBackup(t, c)
	b.Spec.Schedule = "0 4 * * *"
	if err := c.Update(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	got := getCronJob(t, c).Labels
	if got[backup.LabelManagedBy] != backup.ManagedByValue || got[backup.LabelBackup] != "vw" || got["fremd"] != "" {
		t.Fatalf("Labels nicht wiederhergestellt: %v", got)
	}
}

func TestReconcileSuspendedBackup(t *testing.T) {
	b := testBackup()
	b.Spec.Suspend = true
	r, c := newReconciler(t, b)
	if err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	if cj := getCronJob(t, c); cj.Spec.Suspend == nil || !*cj.Spec.Suspend {
		t.Fatal("CronJob nicht pausiert")
	}
	got := getBackup(t, c)
	if got.Status.Phase != v1alpha1.BackupPhaseSuspended {
		t.Fatalf("Phase: %s", got.Status.Phase)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, backup.ConditionHealthy)
	if cond == nil || cond.Status != metav1.ConditionUnknown || cond.Reason != backup.ReasonSuspended {
		t.Fatalf("Healthy-Condition: %+v", cond)
	}
}

func TestReconcileRefusesForeignCronJob(t *testing.T) {
	foreign := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "vw", Namespace: "apps"}}
	r, c := newReconciler(t, testBackup(), foreign)

	if err := reconcile(t, r); err == nil {
		t.Fatal("Fehler erwartet, weil der CronJob nicht dem Backup gehört")
	}

	b := getBackup(t, c)
	cond := meta.FindStatusCondition(b.Status.Conditions, backup.ConditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("Ready=False erwartet: %+v", b.Status.Conditions)
	}
}

func TestReconcileInvalidSpecSetsNotReady(t *testing.T) {
	b := testBackup()
	b.Spec.Sources = nil
	r, c := newReconciler(t, b)

	if err := reconcile(t, r); err == nil {
		t.Fatal("Fehler erwartet")
	}
	got := getBackup(t, c)
	if meta.IsStatusConditionTrue(got.Status.Conditions, backup.ConditionReady) {
		t.Fatal("Ready darf nicht True sein")
	}
}

func TestReconcileReflectsCronJobStatus(t *testing.T) {
	r, c := newReconciler(t, testBackup())
	if err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}

	cj := getCronJob(t, c)
	sched := metav1.NewTime(time.Date(2026, 10, 23, 2, 10, 0, 0, time.UTC))
	done := metav1.NewTime(time.Date(2026, 10, 23, 2, 12, 0, 0, time.UTC))
	cj.Status.LastScheduleTime = &sched
	cj.Status.LastSuccessfulTime = &done
	if err := c.Status().Update(context.Background(), &cj); err != nil {
		t.Fatal(err)
	}
	if err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}

	b := getBackup(t, c)
	if b.Status.Phase != v1alpha1.BackupPhaseSucceeded {
		t.Fatalf("Phase: %s", b.Status.Phase)
	}
	if b.Status.LastSuccessfulTime == nil || !b.Status.LastSuccessfulTime.Equal(&done) {
		t.Fatalf("LastSuccessfulTime: %v", b.Status.LastSuccessfulTime)
	}
	if !meta.IsStatusConditionTrue(b.Status.Conditions, backup.ConditionHealthy) {
		t.Fatalf("Healthy nicht True: %+v", b.Status.Conditions)
	}
}

func TestReconcileMissingBackupIsNoop(t *testing.T) {
	r, _ := newReconciler(t)
	if err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
}
