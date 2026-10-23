package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type IssuerReference struct {
	Name string `json:"name"`
	// +kubebuilder:default=ClusterIssuer
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	Kind string `json:"kind,omitempty"`
}

type CertificateEndpoint struct {
	Address string `json:"address"`
	// +kubebuilder:default=true
	VerifyChain bool `json:"verifyChain,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="has(self.secretRef) || has(self.endpoint)",message="secretRef oder endpoint ist Pflicht"
type ManagedCertificateSpec struct {
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
	Endpoint  *CertificateEndpoint         `json:"endpoint,omitempty"`

	IssuerRef   *IssuerReference `json:"issuerRef,omitempty"`
	DNSNames    []string         `json:"dnsNames,omitempty"`
	IPAddresses []string         `json:"ipAddresses,omitempty"`

	// +kubebuilder:default="720h"
	RenewBefore metav1.Duration `json:"renewBefore,omitempty"`

	RegisterID string `json:"registerId,omitempty"`
}

type ManagedCertificateStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	NotAfter           *metav1.Time       `json:"notAfter,omitempty"`
	DaysRemaining      *int32             `json:"daysRemaining,omitempty"`
	Issuer             string             `json:"issuer,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcert
// +kubebuilder:printcolumn:name="NotAfter",type=date,JSONPath=`.status.notAfter`
// +kubebuilder:printcolumn:name="Days",type=integer,JSONPath=`.status.daysRemaining`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ManagedCertificate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedCertificateSpec   `json:"spec,omitempty"`
	Status ManagedCertificateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ManagedCertificateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedCertificate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedCertificate{}, &ManagedCertificateList{})
}
