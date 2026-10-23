package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SourceType string

const (
	SourceTypeFiles    SourceType = "Files"
	SourceTypeSQLite   SourceType = "SQLite"
	SourceTypePostgres SourceType = "Postgres"
)

type BackupPhase string

const (
	BackupPhasePending   BackupPhase = "Pending"
	BackupPhaseRunning   BackupPhase = "Running"
	BackupPhaseSucceeded BackupPhase = "Succeeded"
	BackupPhaseFailed    BackupPhase = "Failed"
	BackupPhaseSuspended BackupPhase = "Suspended"
)

type BackupRepository struct {
	PVCName           string                   `json:"pvcName"`
	PasswordSecretRef corev1.SecretKeySelector `json:"passwordSecretRef"`
}

type SQLiteSource struct {
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9._][A-Za-z0-9._/-]*$`
	File string `json:"file"`
	// +kubebuilder:default=true
	IntegrityCheck bool `json:"integrityCheck,omitempty"`
}

type PostgresSource struct {
	Host string `json:"host"`
	// +kubebuilder:default=5432
	Port              int32                    `json:"port,omitempty"`
	User              string                   `json:"user"`
	Database          string                   `json:"database"`
	PasswordSecretRef corev1.SecretKeySelector `json:"passwordSecretRef"`
}

// +kubebuilder:validation:XValidation:rule="self.type != 'SQLite' || has(self.sqlite)",message="sqlite ist für type SQLite Pflicht"
// +kubebuilder:validation:XValidation:rule="self.type != 'Postgres' || has(self.postgres)",message="postgres ist für type Postgres Pflicht"
// +kubebuilder:validation:XValidation:rule="self.type == 'Postgres' || has(self.pvcName)",message="pvcName ist für type Files und SQLite Pflicht"
type BackupSource struct {
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=40
	Name string `json:"name"`
	// +kubebuilder:validation:Enum=Files;SQLite;Postgres
	Type     SourceType      `json:"type"`
	PVCName  string          `json:"pvcName,omitempty"`
	SQLite   *SQLiteSource   `json:"sqlite,omitempty"`
	Postgres *PostgresSource `json:"postgres,omitempty"`
}

type BackupRetention struct {
	// +kubebuilder:default=7
	// +kubebuilder:validation:Minimum=0
	KeepDaily int32 `json:"keepDaily,omitempty"`
	// +kubebuilder:default=4
	// +kubebuilder:validation:Minimum=0
	KeepWeekly int32 `json:"keepWeekly,omitempty"`
	// +kubebuilder:default=6
	// +kubebuilder:validation:Minimum=0
	KeepMonthly int32 `json:"keepMonthly,omitempty"`
}

type BackupImages struct {
	Restic   string `json:"restic,omitempty"`
	SQLite   string `json:"sqlite,omitempty"`
	Postgres string `json:"postgres,omitempty"`
}

type BackupSpec struct {
	Schedule string `json:"schedule"`
	// +kubebuilder:default="Europe/Berlin"
	TimeZone string `json:"timeZone,omitempty"`
	Suspend  bool   `json:"suspend,omitempty"`

	Repository BackupRepository `json:"repository"`

	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=name
	Sources []BackupSource `json:"sources"`

	Exclude []string `json:"exclude,omitempty"`

	// +kubebuilder:default={}
	Retention BackupRetention `json:"retention,omitempty"`

	// +kubebuilder:default="5%"
	// +kubebuilder:validation:Pattern=`^(\d+%|\d+/\d+|\d+[kKmMgGtT]?)$`
	CheckReadDataSubset string `json:"checkReadDataSubset,omitempty"`

	Host string `json:"host,omitempty"`

	Images    BackupImages                `json:"images,omitempty"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +kubebuilder:default=3
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`
	// +kubebuilder:default=3
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`
}

type BackupStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              BackupPhase        `json:"phase,omitempty"`
	CronJobName        string             `json:"cronJobName,omitempty"`
	LastScheduleTime   *metav1.Time       `json:"lastScheduleTime,omitempty"`
	LastSuccessfulTime *metav1.Time       `json:"lastSuccessfulTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=bak
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="LastSuccess",type=date,JSONPath=`.status.lastSuccessfulTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Backup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupSpec   `json:"spec,omitempty"`
	Status BackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Backup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Backup{}, &BackupList{})
}
