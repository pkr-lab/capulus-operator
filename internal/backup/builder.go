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
)

type Defaults struct {
	ResticImage   string
	SQLiteImage   string
	PostgresImage string
}

const resticScript = `set -eu
restic cat config >/dev/null 2>&1 || restic init
restic backup --host "$BACKUP_HOST" --tag "$BACKUP_TAG" "$@"
restic forget --host "$BACKUP_HOST" --tag "$BACKUP_TAG" --keep-daily "$KEEP_DAILY" --keep-weekly "$KEEP_WEEKLY" --keep-monthly "$KEEP_MONTHLY" --prune
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
	host := b.Spec.Host
	if host == "" {
		host = b.Name
	}

	volumes := []corev1.Volume{
		{Name: "repo", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: b.Spec.Repository.PVCName}}},
		{Name: "stage", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	resticMounts := []corev1.VolumeMount{
		{Name: "repo", MountPath: repoPath},
		{Name: "stage", MountPath: stagePath},
		{Name: "cache", MountPath: cachePath},
	}

	var initContainers []corev1.Container
	var backupArgs []string
	staged := false

	for _, s := range b.Spec.Sources {
		srcDir := path.Join(sourcesPath, s.Name)
		stageDir := path.Join(stagePath, s.Name)
		volName := "source-" + s.Name

		if s.PVCName != "" {
			volumes = append(volumes, corev1.Volume{
				Name: volName,
				VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: s.PVCName,
					ReadOnly:  true,
				}},
			})
		}
		srcMount := corev1.VolumeMount{Name: volName, MountPath: srcDir, ReadOnly: true}

		switch s.Type {
		case v1alpha1.SourceTypeFiles:
			resticMounts = append(resticMounts, srcMount)
			backupArgs = append(backupArgs, srcDir)

		case v1alpha1.SourceTypeSQLite:
			staged = true
			resticMounts = append(resticMounts, srcMount)
			backupArgs = append(backupArgs, srcDir, "--exclude", path.Join(srcDir, s.SQLite.File)+"*")
			dst := path.Join(stageDir, path.Base(s.SQLite.File))
			initContainers = append(initContainers, corev1.Container{
				Name:            "stage-" + s.Name,
				Image:           images.SQLiteImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"sh", "-c", sqliteScript, "stage", path.Join(srcDir, s.SQLite.File), dst, strconv.FormatBool(s.SQLite.IntegrityCheck)},
				VolumeMounts:    []corev1.VolumeMount{srcMount, {Name: "stage", MountPath: stagePath}},
				Resources:       b.Spec.Resources,
			})

		case v1alpha1.SourceTypePostgres:
			staged = true
			pg := s.Postgres
			dst := path.Join(stageDir, pg.Database+".dump")
			initContainers = append(initContainers, corev1.Container{
				Name:            "stage-" + s.Name,
				Image:           images.PostgresImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{{
					Name:      "PGPASSWORD",
					ValueFrom: &corev1.EnvVarSource{SecretKeyRef: pg.PasswordSecretRef.DeepCopy()},
				}},
				Command:      []string{"sh", "-c", postgresScript, "stage", pg.Host, strconv.Itoa(int(pgPort(pg))), pg.User, pg.Database, dst},
				VolumeMounts: []corev1.VolumeMount{{Name: "stage", MountPath: stagePath}},
				Resources:    b.Spec.Resources,
			})
		}
	}

	if staged {
		backupArgs = append([]string{stagePath}, backupArgs...)
	}
	for _, e := range b.Spec.Exclude {
		backupArgs = append(backupArgs, "--exclude", e)
	}

	restic := corev1.Container{
		Name:            "restic",
		Image:           images.ResticImage,
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
		Command:      append([]string{"sh", "-c", resticScript, "restic-backup"}, backupArgs...),
		Resources:    b.Spec.Resources,
		VolumeMounts: resticMounts,
	}

	labels := Labels(b)

	cj := &batchv1.CronJob{
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
					BackoffLimit: ptr.To(int32(2)),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec: corev1.PodSpec{
							RestartPolicy:  corev1.RestartPolicyNever,
							InitContainers: initContainers,
							Containers:     []corev1.Container{restic},
							Volumes:        volumes,
						},
					},
				},
			},
		},
	}
	return cj, nil
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
		return 5432
	}
	return p.Port
}

func checkSubset(b *v1alpha1.Backup) string {
	if b.Spec.CheckReadDataSubset == "" {
		return "5%"
	}
	return b.Spec.CheckReadDataSubset
}

func timeZone(b *v1alpha1.Backup) string {
	if b.Spec.TimeZone == "" {
		return "Europe/Berlin"
	}
	return b.Spec.TimeZone
}

func historyLimit(v *int32) int32 {
	if v == nil {
		return 3
	}
	return *v
}
