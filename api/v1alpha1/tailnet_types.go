package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TailnetSpec defines the desired state of Tailnet.
type TailnetSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:default="-"
	TailnetName string `json:"tailnetName"`

	// +kubebuilder:validation:Required
	OAuthSecretRef SecretReference `json:"oauthSecretRef"`

	// +kubebuilder:validation:Required
	Tailscale TailscaleConfig `json:"tailscale"`
}

// SecretReference references a secret containing OAuth credentials.
type SecretReference struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

// TailscaleConfig contains Tailscale-specific configuration.
type TailscaleConfig struct {
	// +kubebuilder:default=true
	// +optional
	AutoApprove bool `json:"autoApprove,omitempty"`

	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +optional
	HostnamePrefix string `json:"hostnamePrefix,omitempty"`

	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// +optional
	Tags []string `json:"tags,omitempty"`

	// +kubebuilder:default="ghcr.io/tailscale/tailscale:latest"
	// +optional
	Image string `json:"image,omitempty"`
}

// TailnetStatus defines the observed state of Tailnet.
type TailnetStatus struct {
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	AuthKeyID string `json:"authKeyID,omitempty"`

	// +optional
	AuthKeyCreated *metav1.Time `json:"authKeyCreated,omitempty"`

	// +optional
	InjectedPods int32 `json:"injectedPods,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Tailnet",type=string,JSONPath=`.spec.tailnetName`
// +kubebuilder:printcolumn:name="Injected",type=integer,JSONPath=`.status.injectedPods`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Tailnet is the Schema for the tailnets API.
type Tailnet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TailnetSpec   `json:"spec,omitempty"`
	Status TailnetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TailnetList contains a list of Tailnet.
type TailnetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tailnet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tailnet{}, &TailnetList{})
}
