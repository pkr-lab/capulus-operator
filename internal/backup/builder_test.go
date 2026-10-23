package backup

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/pkr-lab/capulus-operator/api/v1alpha1"
)

var testDefaults = Defaults{
	ResticImage:   "restic/restic:test",
	SQLiteImage:   "alpine:test",
	PostgresImage: "postgres:test",
}

func newBackup(sources ...v1alpha1.BackupSource) *v1alpha1.Backup {
	return &v1alpha1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "vw", Namespace: "apps"},
		Spec: v1alpha1.BackupSpec{
			Schedule: "10 2 * * *",
			Repository: v1alpha1.BackupRepository{
				PVCName:           "vw-restic",
				PasswordSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "vw-restic"}, Key: "password"},
			},
			Retention: v1alpha1.BackupRetention{KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 6},
			Sources:   sources,
		},
	}
}

func sqliteSource() v1alpha1.BackupSource {
	return v1alpha1.BackupSource{
		Name:    "data",
		Type:    v1alpha1.SourceTypeSQLite,
		PVCName: "vw-data",
		SQLite:  &v1alpha1.SQLiteSource{File: "db.sqlite3", IntegrityCheck: true},
	}
}

func postgresSource() v1alpha1.BackupSource {
	return v1alpha1.BackupSource{
		Name: "db",
		Type: v1alpha1.SourceTypePostgres,
		Postgres: &v1alpha1.PostgresSource{
			Host:              "pg",
			User:              "app",
			Database:          "appdb",
			PasswordSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "pg"}, Key: "pw"},
		},
	}
}

func filesSource() v1alpha1.BackupSource {
	return v1alpha1.BackupSource{Name: "files", Type: v1alpha1.SourceTypeFiles, PVCName: "files-pvc"}
}

func envValue(c corev1.Container, name string) string {
	for _, e := range c.Env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

func TestBuildCronJobSQLite(t *testing.T) {
	b := newBackup(sqliteSource())
	b.Spec.Exclude = []string{"/sources/data/tmp"}
	cj, err := BuildCronJob(b, testDefaults)
	if err != nil {
		t.Fatal(err)
	}

	if cj.Name != "vw" || cj.Namespace != "apps" {
		t.Fatalf("Name/Namespace falsch: %s/%s", cj.Namespace, cj.Name)
	}
	if cj.Spec.Schedule != "10 2 * * *" || *cj.Spec.TimeZone != "Europe/Berlin" {
		t.Fatalf("Zeitplan falsch: %s %s", cj.Spec.Schedule, *cj.Spec.TimeZone)
	}
	if cj.Spec.ConcurrencyPolicy != "Forbid" {
		t.Fatalf("ConcurrencyPolicy: %s", cj.Spec.ConcurrencyPolicy)
	}
	if cj.Labels[LabelManagedBy] != ManagedByValue || cj.Labels[LabelBackup] != "vw" {
		t.Fatalf("Labels falsch: %v", cj.Labels)
	}

	pod := cj.Spec.JobTemplate.Spec.Template.Spec
	if len(pod.InitContainers) != 1 {
		t.Fatalf("erwartet 1 InitContainer, erhalten %d", len(pod.InitContainers))
	}
	init := pod.InitContainers[0]
	if init.Image != "alpine:test" || init.Name != "stage-data" {
		t.Fatalf("InitContainer: %s %s", init.Name, init.Image)
	}
	wantInitArgs := []string{"stage", "/sources/data/db.sqlite3", "/stage/data/db.sqlite3", "true"}
	if !slices.Equal(init.Command[3:], wantInitArgs) {
		t.Fatalf("Init-Argumente: %v", init.Command[3:])
	}

	restic := pod.Containers[0]
	if restic.Image != "restic/restic:test" {
		t.Fatalf("restic-Image: %s", restic.Image)
	}
	wantArgs := []string{"/stage", "/sources/data", "--exclude", "/sources/data/db.sqlite3*", "--exclude", "/sources/data/tmp"}
	if !slices.Equal(restic.Command[4:], wantArgs) {
		t.Fatalf("restic-Argumente: %v", restic.Command[4:])
	}
	if envValue(restic, "BACKUP_HOST") != "vw" || envValue(restic, "BACKUP_TAG") != "vw" {
		t.Fatalf("Host/Tag falsch")
	}
	if envValue(restic, "KEEP_DAILY") != "7" || envValue(restic, "KEEP_WEEKLY") != "4" || envValue(restic, "KEEP_MONTHLY") != "6" {
		t.Fatalf("Retention falsch")
	}
	if envValue(restic, "CHECK_SUBSET") != "5%" {
		t.Fatalf("CHECK_SUBSET: %s", envValue(restic, "CHECK_SUBSET"))
	}

	var sourceVol *corev1.Volume
	for i := range pod.Volumes {
		if pod.Volumes[i].Name == "source-data" {
			sourceVol = &pod.Volumes[i]
		}
	}
	if sourceVol == nil || !sourceVol.PersistentVolumeClaim.ReadOnly || sourceVol.PersistentVolumeClaim.ClaimName != "vw-data" {
		t.Fatalf("Quell-Volume falsch: %+v", sourceVol)
	}
}

func TestBuildCronJobPostgres(t *testing.T) {
	cj, err := BuildCronJob(newBackup(postgresSource()), testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	pod := cj.Spec.JobTemplate.Spec.Template.Spec
	init := pod.InitContainers[0]
	if init.Image != "postgres:test" {
		t.Fatalf("Image: %s", init.Image)
	}
	want := []string{"stage", "pg", "5432", "app", "appdb", "/stage/db/appdb.dump"}
	if !slices.Equal(init.Command[3:], want) {
		t.Fatalf("Argumente: %v", init.Command[3:])
	}
	if init.Env[0].Name != "PGPASSWORD" || init.Env[0].ValueFrom.SecretKeyRef.Name != "pg" {
		t.Fatalf("PGPASSWORD nicht aus Secret: %+v", init.Env)
	}
	if got := pod.Containers[0].Command[4:]; !slices.Equal(got, []string{"/stage"}) {
		t.Fatalf("restic-Argumente: %v", got)
	}
}

func TestBuildCronJobFilesOnlyHasNoStage(t *testing.T) {
	cj, err := BuildCronJob(newBackup(filesSource()), testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	pod := cj.Spec.JobTemplate.Spec.Template.Spec
	if len(pod.InitContainers) != 0 {
		t.Fatalf("keine InitContainer erwartet")
	}
	if got := pod.Containers[0].Command[4:]; !slices.Equal(got, []string{"/sources/files"}) {
		t.Fatalf("restic-Argumente: %v", got)
	}
}

func TestBuildCronJobImageOverride(t *testing.T) {
	b := newBackup(postgresSource())
	b.Spec.Images.Postgres = "postgres:18"
	b.Spec.Images.Restic = "restic/restic:custom"
	b.Spec.Host = "custom-host"
	b.Spec.CheckReadDataSubset = "10%"
	cj, err := BuildCronJob(b, testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	pod := cj.Spec.JobTemplate.Spec.Template.Spec
	if pod.InitContainers[0].Image != "postgres:18" || pod.Containers[0].Image != "restic/restic:custom" {
		t.Fatalf("Images nicht überschrieben")
	}
	if envValue(pod.Containers[0], "BACKUP_HOST") != "custom-host" || envValue(pod.Containers[0], "CHECK_SUBSET") != "10%" {
		t.Fatalf("Host/Subset nicht übernommen")
	}
}

func TestBuildCronJobMixedSources(t *testing.T) {
	cj, err := BuildCronJob(newBackup(sqliteSource(), postgresSource(), filesSource()), testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	pod := cj.Spec.JobTemplate.Spec.Template.Spec
	if len(pod.InitContainers) != 2 {
		t.Fatalf("erwartet 2 InitContainer, erhalten %d", len(pod.InitContainers))
	}
	args := pod.Containers[0].Command[4:]
	if args[0] != "/stage" || !slices.Contains(args, "/sources/files") || !slices.Contains(args, "/sources/data") {
		t.Fatalf("restic-Argumente: %v", args)
	}
	names := map[string]bool{}
	for _, v := range pod.Volumes {
		if names[v.Name] {
			t.Fatalf("Volume %s doppelt", v.Name)
		}
		names[v.Name] = true
	}
}

func TestBuildCronJobValidation(t *testing.T) {
	cases := map[string]*v1alpha1.Backup{
		"keine Quelle":      newBackup(),
		"doppelter Name":    newBackup(filesSource(), filesSource()),
		"Files ohne PVC":    newBackup(v1alpha1.BackupSource{Name: "x", Type: v1alpha1.SourceTypeFiles}),
		"SQLite ohne Datei": newBackup(v1alpha1.BackupSource{Name: "x", Type: v1alpha1.SourceTypeSQLite, PVCName: "p"}),
		"Postgres ohne Cfg": newBackup(v1alpha1.BackupSource{Name: "x", Type: v1alpha1.SourceTypePostgres}),
		"unbekannter Typ":   newBackup(v1alpha1.BackupSource{Name: "x", Type: "Mongo"}),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildCronJob(b, testDefaults); err == nil {
				t.Fatal("Fehler erwartet")
			}
		})
	}
}

func TestSpecHashStable(t *testing.T) {
	a, _ := BuildCronJob(newBackup(sqliteSource()), testDefaults)
	b, _ := BuildCronJob(newBackup(sqliteSource()), testDefaults)
	ha, _ := SpecHash(a)
	hb, _ := SpecHash(b)
	if ha != hb {
		t.Fatal("Hash nicht deterministisch")
	}
	changed := newBackup(sqliteSource())
	changed.Spec.Schedule = "0 4 * * *"
	c, _ := BuildCronJob(changed, testDefaults)
	hc, _ := SpecHash(c)
	if ha == hc {
		t.Fatal("Hash ändert sich nicht bei geändertem Zeitplan")
	}
}
