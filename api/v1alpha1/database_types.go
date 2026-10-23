package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DatabaseEngine string

const (
	DatabaseEnginePostgres DatabaseEngine = "postgres"
	DatabaseEngineMySQL    DatabaseEngine = "mysql"
)

type DatabaseStorage struct {
	Size             resource.Quantity `json:"size"`
	StorageClassName *string           `json:"storageClassName,omitempty"`
}

type DatabaseSpec struct {
	// +kubebuilder:validation:Enum=postgres;mysql
	Engine DatabaseEngine `json:"engine"`
	Image  string         `json:"image"`

	DatabaseName      string                   `json:"databaseName"`
	Username          string                   `json:"username"`
	PasswordSecretRef corev1.SecretKeySelector `json:"passwordSecretRef"`

	Storage   DatabaseStorage             `json:"storage"`
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	BackupRef *corev1.LocalObjectReference `json:"backupRef,omitempty"`
}

type DatabaseStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	ServiceName        string             `json:"serviceName,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=db
// +kubebuilder:printcolumn:name="Engine",type=string,JSONPath=`.spec.engine`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Database struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseSpec   `json:"spec,omitempty"`
	Status DatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type DatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Database `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Database{}, &DatabaseList{})
}
