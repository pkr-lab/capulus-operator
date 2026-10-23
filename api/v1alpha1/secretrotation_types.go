package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RotationStrategy string

const (
	RotationStrategyGenerated RotationStrategy = "Generated"
	RotationStrategyProvider  RotationStrategy = "Provider"
)

type GeneratedSecret struct {
	// +kubebuilder:default=48
	// +kubebuilder:validation:Minimum=16
	Length int32 `json:"length,omitempty"`
}

type GitTarget struct {
	Repository string `json:"repository"`
	// +kubebuilder:default=main
	Branch string `json:"branch,omitempty"`
	Path   string `json:"path"`

	TokenSecretRef corev1.SecretKeySelector `json:"tokenSecretRef"`
}

type RotationConsumer struct {
	// +kubebuilder:validation:Enum=Deployment;StatefulSet;DaemonSet
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// +kubebuilder:validation:XValidation:rule="self.strategy != 'Generated' || has(self.generated)",message="generated ist für strategy Generated Pflicht"
// +kubebuilder:validation:XValidation:rule="self.strategy != 'Provider' || has(self.provider)",message="provider ist für strategy Provider Pflicht"
type SecretRotationSpec struct {
	SecretRef corev1.SecretKeySelector `json:"secretRef"`

	// +kubebuilder:validation:Enum=Generated;Provider
	Strategy  RotationStrategy `json:"strategy"`
	Generated *GeneratedSecret `json:"generated,omitempty"`
	Provider  string           `json:"provider,omitempty"`

	MaxAge metav1.Duration `json:"maxAge"`

	Git GitTarget `json:"git"`

	Consumers []RotationConsumer `json:"consumers,omitempty"`

	RegisterID string `json:"registerId,omitempty"`
	Suspend    bool   `json:"suspend,omitempty"`
}

type SecretRotationStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	LastRotated        *metav1.Time       `json:"lastRotated,omitempty"`
	NextRotation       *metav1.Time       `json:"nextRotation,omitempty"`
	PullRequestURL     string             `json:"pullRequestURL,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=srot
// +kubebuilder:printcolumn:name="Strategy",type=string,JSONPath=`.spec.strategy`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Next",type=date,JSONPath=`.status.nextRotation`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type SecretRotation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecretRotationSpec   `json:"spec,omitempty"`
	Status SecretRotationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SecretRotationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecretRotation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecretRotation{}, &SecretRotationList{})
}
