package backup

import (
	"fmt"
	"path"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/pkr-lab/capulus-operator/api/v1alpha1"
)

const (
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelBackup    = "capulus.homeserver/backup"
	ManagedByValue = "capulus-operator"

	stagePath   = "/stage"
	sourcesPath = "/sources"
	repoPath    = "/repo"
	cachePath   = "/cache"

	volumeRepo           = "repo"
	volumeStage          = "stage"
	volumeCache          = "cache"
	sourceVolumePrefix   = "source-"
	stageContainerPrefix = "stage-"

	defaultPostgresPort = 5432
	defaultCheckSubset  = "5%"
	defaultTimeZone     = "Europe/Berlin"
	defaultHistoryLimit = 3
	jobBackoffLimit     = 2
)

type Defaults struct {
	ResticImage   string
	SQLiteImage   string
	PostgresImage string
}

const resticScript = `set -eu
restic cat config >/dev/null 2>&1 || restic init
restic backup --host "$BACKUP_HOST" --tag "$BACKUP_TAG" "$@"
restic forget --host "$BACKUP_HOST" --tag "$BACKUP_TAG" --group-by host,tags --keep-daily "$KEEP_DAILY" --keep-weekly "$KEEP_WEEKLY" --keep-monthly "$KEEP_MONTHLY" --prune
restic check --read-data-subset="$CHECK_SUBSET"
echo "fertig: $(date -Iseconds)"
`

const sqliteScript = `set -eu
apk add --no-cache sqlite >/dev/null
src="$1"; dst="$2"; check="$3"
mkdir -p "$(dirname "$dst")"
sqlite3 -readonly "$src" ".backup '$dst'"
if [ "$check" = "true" ]; then
  sqlite3 "$dst" "pragma integrity_check" | grep -qx ok
fi
`

const postgresScript = `set -eu
for i in $(seq 1 15); do
  pg_isready -h "$1" -p "$2" -U "$3" && break
  sleep 2
done
mkdir -p "$(dirname "$5")"
pg_dump -h "$1" -p "$2" -U "$3" -d "$4" -F c -f "$5"
`

func Labels(b *v1alpha1.Backup) map[string]string {
	return map[string]string{
		LabelManagedBy: ManagedByValue,
		LabelBackup:    b.Name,
	}
}

func BuildCronJob(b *v1alpha1.Backup, d Defaults) (*batchv1.CronJob, error) {
	if err := validate(b); err != nil {
		return nil, err
	}

	images := resolveImages(b, d)
	plan := planSources(b, images)
	podSpec := corev1.PodSpec{
		RestartPolicy:  corev1.RestartPolicyNever,
		InitContainers: plan.initContainers,
		Containers:     []corev1.Container{resticContainer(b, images.ResticImage, plan)},
		Volumes:        append(baseVolumes(b), plan.volumes...),
	}
	return cronJobFor(b, podSpec), nil
}

type sourcePlan struct {
	volumes        []corev1.Volume
	resticMounts   []corev1.VolumeMount
	initContainers []corev1.Container
	backupArgs     []string
}

func planSources(b *v1alpha1.Backup, images Defaults) sourcePlan {
	var plan sourcePlan
	staged := false

	for _, s := range b.Spec.Sources {
		srcDir := path.Join(sourcesPath, s.Name)
		srcMount := corev1.VolumeMount{Name: sourceVolumePrefix + s.Name, MountPath: srcDir, ReadOnly: true}
		if s.PVCName != "" {
			plan.volumes = append(plan.volumes, sourceVolume(s))
		}

		switch s.Type {
		case v1alpha1.SourceTypeFiles:
			plan.resticMounts = append(plan.resticMounts, srcMount)
			plan.backupArgs = append(plan.backupArgs, srcDir)

		case v1alpha1.SourceTypeSQLite:
			staged = true
			plan.resticMounts = append(plan.resticMounts, srcMount)
			plan.backupArgs = append(plan.backupArgs, srcDir, "--exclude", path.Join(srcDir, s.SQLite.File)+"*")
			plan.initContainers = append(plan.initContainers, sqliteStageContainer(s, srcMount, images.SQLiteImage, b.Spec.Resources))

		case v1alpha1.SourceTypePostgres:
			staged = true
			plan.initContainers = append(plan.initContainers, postgresStageContainer(s, images.PostgresImage, b.Spec.Resources))
		}
	}

	if staged {
		plan.backupArgs = append([]string{stagePath}, plan.backupArgs...)
	}
	for _, e := range b.Spec.Exclude {
		plan.backupArgs = append(plan.backupArgs, "--exclude", e)
	}
	return plan
}

func baseVolumes(b *v1alpha1.Backup) []corev1.Volume {
	return []corev1.Volume{
		{Name: volumeRepo, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: b.Spec.Repository.PVCName}}},
		{Name: volumeStage, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: volumeCache, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
}

func sourceVolume(s v1alpha1.BackupSource) corev1.Volume {
	return corev1.Volume{
		Name: sourceVolumePrefix + s.Name,
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: s.PVCName,
			ReadOnly:  true,
		}},
	}
}

func stageMount() corev1.VolumeMount {
	return corev1.VolumeMount{Name: volumeStage, MountPath: stagePath}
}

func sqliteStageContainer(s v1alpha1.BackupSource, srcMount corev1.VolumeMount, image string, res corev1.ResourceRequirements) corev1.Container {
	src := path.Join(srcMount.MountPath, s.SQLite.File)
	dst := path.Join(stagePath, s.Name, path.Base(s.SQLite.File))
	return corev1.Container{
		Name:            stageContainerPrefix + s.Name,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-c", sqliteScript, "stage", src, dst, strconv.FormatBool(s.SQLite.IntegrityCheck)},
		VolumeMounts:    []corev1.VolumeMount{srcMount, stageMount()},
		Resources:       res,
	}
}

func postgresStageContainer(s v1alpha1.BackupSource, image string, res corev1.ResourceRequirements) corev1.Container {
	pg := s.Postgres
	dst := path.Join(stagePath, s.Name, pg.Database+".dump")
	return corev1.Container{
		Name:            stageContainerPrefix + s.Name,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{{
			Name:      "PGPASSWORD",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: pg.PasswordSecretRef.DeepCopy()},
		}},
		Command:      []string{"sh", "-c", postgresScript, "stage", pg.Host, strconv.Itoa(int(pgPort(pg))), pg.User, pg.Database, dst},
		VolumeMounts: []corev1.VolumeMount{stageMount()},
		Resources:    res,
	}
}

func resticContainer(b *v1alpha1.Backup, image string, plan sourcePlan) corev1.Container {
	host := b.Spec.Host
	if host == "" {
		host = b.Name
	}
	mounts := append([]corev1.VolumeMount{
		{Name: volumeRepo, MountPath: repoPath},
		stageMount(),
		{Name: volumeCache, MountPath: cachePath},
	}, plan.resticMounts...)
	return corev1.Container{
		Name:            "restic",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{Name: "RESTIC_REPOSITORY", Value: repoPath},
			{Name: "RESTIC_CACHE_DIR", Value: cachePath},
			{Name: "RESTIC_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: b.Spec.Repository.PasswordSecretRef.DeepCopy()}},
			{Name: "BACKUP_HOST", Value: host},
			{Name: "BACKUP_TAG", Value: b.Name},
			{Name: "KEEP_DAILY", Value: strconv.Itoa(int(b.Spec.Retention.KeepDaily))},
			{Name: "KEEP_WEEKLY", Value: strconv.Itoa(int(b.Spec.Retention.KeepWeekly))},
			{Name: "KEEP_MONTHLY", Value: strconv.Itoa(int(b.Spec.Retention.KeepMonthly))},
			{Name: "CHECK_SUBSET", Value: checkSubset(b)},
		},
		Command:      append([]string{"sh", "-c", resticScript, "restic-backup"}, plan.backupArgs...),
		Resources:    b.Spec.Resources,
		VolumeMounts: mounts,
	}
}

func cronJobFor(b *v1alpha1.Backup, podSpec corev1.PodSpec) *batchv1.CronJob {
	labels := Labels(b)
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.Name,
			Namespace: b.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   b.Spec.Schedule,
			TimeZone:                   ptr.To(timeZone(b)),
			Suspend:                    ptr.To(b.Spec.Suspend),
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			FailedJobsHistoryLimit:     ptr.To(historyLimit(b.Spec.FailedJobsHistoryLimit)),
			SuccessfulJobsHistoryLimit: ptr.To(historyLimit(b.Spec.SuccessfulJobsHistoryLimit)),
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: batchv1.JobSpec{
					BackoffLimit: ptr.To(int32(jobBackoffLimit)),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       podSpec,
					},
				},
			},
		},
	}
}

func validate(b *v1alpha1.Backup) error {
	if len(b.Spec.Sources) == 0 {
		return fmt.Errorf("mindestens eine Quelle nötig")
	}
	seen := map[string]bool{}
	for _, s := range b.Spec.Sources {
		if seen[s.Name] {
			return fmt.Errorf("Quelle %q doppelt", s.Name)
		}
		seen[s.Name] = true
		switch s.Type {
		case v1alpha1.SourceTypeFiles:
			if s.PVCName == "" {
				return fmt.Errorf("Quelle %q: pvcName fehlt", s.Name)
			}
		case v1alpha1.SourceTypeSQLite:
			if s.PVCName == "" || s.SQLite == nil || s.SQLite.File == "" {
				return fmt.Errorf("Quelle %q: pvcName und sqlite.file nötig", s.Name)
			}
		case v1alpha1.SourceTypePostgres:
			if s.Postgres == nil {
				return fmt.Errorf("Quelle %q: postgres fehlt", s.Name)
			}
		default:
			return fmt.Errorf("Quelle %q: unbekannter Typ %q", s.Name, s.Type)
		}
	}
	return nil
}

func resolveImages(b *v1alpha1.Backup, d Defaults) Defaults {
	out := d
	if b.Spec.Images.Restic != "" {
		out.ResticImage = b.Spec.Images.Restic
	}
	if b.Spec.Images.SQLite != "" {
		out.SQLiteImage = b.Spec.Images.SQLite
	}
	if b.Spec.Images.Postgres != "" {
		out.PostgresImage = b.Spec.Images.Postgres
	}
	return out
}

func pgPort(p *v1alpha1.PostgresSource) int32 {
	if p.Port == 0 {
		return defaultPostgresPort
	}
	return p.Port
}

func checkSubset(b *v1alpha1.Backup) string {
	if b.Spec.CheckReadDataSubset == "" {
		return defaultCheckSubset
	}
	return b.Spec.CheckReadDataSubset
}

func timeZone(b *v1alpha1.Backup) string {
	if b.Spec.TimeZone == "" {
		return defaultTimeZone
	}
	return b.Spec.TimeZone
}

func historyLimit(v *int32) int32 {
	if v == nil {
		return defaultHistoryLimit
	}
	return *v
}
