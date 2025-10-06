package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TailserveSpec defines the desired state of Tailserve.
type TailserveSpec struct {
	// ServiceName is the name of the Tailscale service (e.g., "web-server" becomes "svc:web-server")
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ServiceName string `json:"serviceName"`

	// TailnetRef references the Tailnet resource to use
	// +kubebuilder:validation:Required
	TailnetRef string `json:"tailnetRef"`

	// Handlers defines the service proxy configuration
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Handlers []ServiceHandler `json:"handlers"`
}

// ServiceHandler defines a handler for the service proxy.
type ServiceHandler struct {
	// Port is the port to expose the service on
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	// Protocol specifies the protocol to use (http, https, tcp, tls-terminated-tcp)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=http;https;tcp;tls-terminated-tcp
	Protocol string `json:"protocol"`

	// Routes defines path-based routing for HTTP/HTTPS handlers
	// +optional
	Routes []Route `json:"routes,omitempty"`

	// TCPProxy defines the backend for TCP/TLS-terminated-TCP handlers
	// +optional
	TCPProxy *TCPProxy `json:"tcpProxy,omitempty"`
}

// Route defines a path-based route for HTTP/HTTPS handlers.
type Route struct {
	// Path is the URL path to match (defaults to "/")
	// +kubebuilder:default="/"
	// +optional
	Path string `json:"path,omitempty"`

	// Backend defines where to proxy requests
	// +kubebuilder:validation:Required
	Backend Backend `json:"backend"`
}

// Backend defines the backend configuration for a route.
type Backend struct {
	// Type specifies the backend type (proxy, text, file)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=proxy;text;file
	Type string `json:"type"`

	// Proxy is the backend address for type=proxy (e.g., "http://127.0.0.1:8080")
	// +optional
	Proxy string `json:"proxy,omitempty"`

	// Text is the text content to serve for type=text
	// +optional
	Text string `json:"text,omitempty"`

	// File is the file path to serve for type=file
	// +optional
	File string `json:"file,omitempty"`
}

// TCPProxy defines the backend for TCP handlers.
type TCPProxy struct {
	// Backend is the backend address (e.g., "tcp://localhost:5432")
	// +kubebuilder:validation:Required
	Backend string `json:"backend"`

	// TerminateTLS enables TLS termination before forwarding to the backend.
	// When enabled for tls-terminated-tcp protocol, TLS is terminated and plaintext is forwarded.
	// The service's FQDN is used as the SNI hostname for certificate validation.
	// +optional
	TerminateTLS bool `json:"terminateTLS,omitempty"`
}

// TailserveStatus defines the observed state of Tailserve.
type TailserveStatus struct {
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ConfigMapName is the name of the generated ConfigMap
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`

	// LastUpdated is the last time the serve config was updated
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// ObservedGeneration reflects the generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=`.spec.serviceName`
// +kubebuilder:printcolumn:name="Tailnet",type=string,JSONPath=`.spec.tailnetRef`
// +kubebuilder:printcolumn:name="ConfigMap",type=string,JSONPath=`.status.configMapName`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Tailserve is the Schema for the tailserves API.
type Tailserve struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TailserveSpec   `json:"spec,omitempty"`
	Status TailserveStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TailserveList contains a list of Tailserve.
type TailserveList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tailserve `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tailserve{}, &TailserveList{})
}
