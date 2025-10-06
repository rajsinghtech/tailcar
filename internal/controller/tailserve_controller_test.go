package controller

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
)

func TestTailserveReconciler_GenerateServeConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name      string
		tailserve *tailcarv1alpha1.Tailserve
		want      map[string]interface{}
	}{
		{
			name: "HTTPS handler with proxy",
			tailserve: &tailcarv1alpha1.Tailserve{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "web-server",
					TailnetRef:  "my-tailnet",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     443,
							Protocol: "https",
							Routes: []tailcarv1alpha1.Route{
								{
									Path: "/",
									Backend: tailcarv1alpha1.Backend{
										Type:  "proxy",
										Proxy: "http://127.0.0.1:8080",
									},
								},
							},
						},
					},
				},
			},
			want: map[string]interface{}{
				"Services": map[string]interface{}{
					"svc:web-server": map[string]interface{}{
						"TCP": map[string]interface{}{
							"443": map[string]interface{}{
								"HTTPS": true,
							},
						},
						"Web": map[string]interface{}{
							"web-server.example.ts.net:443": map[string]interface{}{
								"Handlers": map[string]interface{}{
									"/": map[string]interface{}{
										"Proxy": "http://127.0.0.1:8080",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "TCP handler",
			tailserve: &tailcarv1alpha1.Tailserve{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-tcp",
				},
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "postgres",
					TailnetRef:  "my-tailnet",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     5432,
							Protocol: "tcp",
							TCPProxy: &tailcarv1alpha1.TCPProxy{
								Backend: "tcp://localhost:5432",
							},
						},
					},
				},
			},
			want: map[string]interface{}{
				"Services": map[string]interface{}{
					"svc:postgres": map[string]interface{}{
						"TCP": map[string]interface{}{
							"5432": map[string]interface{}{
								"TCPForward": "tcp://localhost:5432",
							},
						},
						"Web": map[string]interface{}{},
					},
				},
			},
		},
		{
			name: "Multiple routes",
			tailserve: &tailcarv1alpha1.Tailserve{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-multi",
				},
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "api",
					TailnetRef:  "my-tailnet",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     443,
							Protocol: "https",
							Routes: []tailcarv1alpha1.Route{
								{
									Path: "/",
									Backend: tailcarv1alpha1.Backend{
										Type:  "proxy",
										Proxy: "http://127.0.0.1:8080",
									},
								},
								{
									Path: "/api",
									Backend: tailcarv1alpha1.Backend{
										Type:  "proxy",
										Proxy: "http://127.0.0.1:3000",
									},
								},
								{
									Path: "/status",
									Backend: tailcarv1alpha1.Backend{
										Type: "text",
										Text: "OK",
									},
								},
							},
						},
					},
				},
			},
			want: map[string]interface{}{
				"Services": map[string]interface{}{
					"svc:api": map[string]interface{}{
						"TCP": map[string]interface{}{
							"443": map[string]interface{}{
								"HTTPS": true,
							},
						},
						"Web": map[string]interface{}{
							"api.example.ts.net:443": map[string]interface{}{
								"Handlers": map[string]interface{}{
									"/": map[string]interface{}{
										"Proxy": "http://127.0.0.1:8080",
									},
									"/api": map[string]interface{}{
										"Proxy": "http://127.0.0.1:3000",
									},
									"/status": map[string]interface{}{
										"Text": "OK",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &TailserveReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
				Scheme: scheme,
			}

			configJSON, err := r.generateServeConfig(tt.tailserve, "example.ts.net")
			if err != nil {
				t.Fatalf("generateServeConfig() error = %v", err)
			}

			var got map[string]interface{}
			if err := json.Unmarshal([]byte(configJSON), &got); err != nil {
				t.Fatalf("Failed to unmarshal generated config: %v", err)
			}

			gotJSON, _ := json.MarshalIndent(got, "", "  ")
			wantJSON, _ := json.MarshalIndent(tt.want, "", "  ")

			if string(gotJSON) != string(wantJSON) {
				t.Errorf("generateServeConfig() mismatch\nGot:\n%s\n\nWant:\n%s", gotJSON, wantJSON)
			}
		})
	}
}

func TestTailserveReconciler_BuildServiceConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	r := &TailserveReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}

	tests := []struct {
		name      string
		tailserve *tailcarv1alpha1.Tailserve
		wantTCP   bool
		wantWeb   bool
	}{
		{
			name: "HTTP handler",
			tailserve: &tailcarv1alpha1.Tailserve{
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "web",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     80,
							Protocol: "http",
							Routes: []tailcarv1alpha1.Route{
								{
									Path: "/",
									Backend: tailcarv1alpha1.Backend{
										Type:  "proxy",
										Proxy: "http://127.0.0.1:8080",
									},
								},
							},
						},
					},
				},
			},
			wantTCP: true,
			wantWeb: true,
		},
		{
			name: "TCP handler",
			tailserve: &tailcarv1alpha1.Tailserve{
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "db",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     5432,
							Protocol: "tcp",
							TCPProxy: &tailcarv1alpha1.TCPProxy{
								Backend: "tcp://localhost:5432",
							},
						},
					},
				},
			},
			wantTCP: true,
			wantWeb: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := r.buildServiceConfig(tt.tailserve, "example.ts.net")

			if tt.wantTCP {
				if _, ok := config["TCP"]; !ok {
					t.Error("Expected TCP configuration")
				}
			}

			if tt.wantWeb {
				if _, ok := config["Web"]; !ok {
					t.Error("Expected Web configuration")
				}
			}
		})
	}
}

func TestTailserveReconciler_ExtractPorts(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name      string
		tailserve *tailcarv1alpha1.Tailserve
		want      []string
	}{
		{
			name: "http handler",
			tailserve: &tailcarv1alpha1.Tailserve{
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "test-service",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     443,
							Protocol: "https",
						},
					},
				},
			},
			want: []string{"tcp:443"},
		},
		{
			name: "multiple handlers with different ports",
			tailserve: &tailcarv1alpha1.Tailserve{
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "test-service",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     443,
							Protocol: "https",
						},
						{
							Port:     80,
							Protocol: "http",
						},
						{
							Port:     8080,
							Protocol: "tcp",
						},
					},
				},
			},
			want: []string{"tcp:443", "tcp:80", "tcp:8080"},
		},
		{
			name: "tls-terminated-tcp handler",
			tailserve: &tailcarv1alpha1.Tailserve{
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "test-service",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     5432,
							Protocol: "tls-terminated-tcp",
						},
					},
				},
			},
			want: []string{"tcp:5432"},
		},
		{
			name: "duplicate ports deduped",
			tailserve: &tailcarv1alpha1.Tailserve{
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "test-service",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     443,
							Protocol: "https",
						},
						{
							Port:     443,
							Protocol: "https",
						},
					},
				},
			},
			want: []string{"tcp:443"},
		},
		{
			name: "no handlers returns default",
			tailserve: &tailcarv1alpha1.Tailserve{
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "test-service",
					Handlers:    []tailcarv1alpha1.ServiceHandler{},
				},
			},
			want: []string{"tcp:443"},
		},
		{
			name: "port format uses colon not slash",
			tailserve: &tailcarv1alpha1.Tailserve{
				Spec: tailcarv1alpha1.TailserveSpec{
					ServiceName: "test-service",
					Handlers: []tailcarv1alpha1.ServiceHandler{
						{
							Port:     8080,
							Protocol: "http",
						},
					},
				},
			},
			want: []string{"tcp:8080"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &TailserveReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
				Scheme: scheme,
			}
			got := r.extractPorts(tt.tailserve)

			// Check that all expected ports are present
			if len(got) != len(tt.want) {
				t.Errorf("extractPorts() returned %d ports, want %d. Got: %v, Want: %v", len(got), len(tt.want), got, tt.want)
				return
			}

			// Convert to map for easier comparison (order doesn't matter)
			wantMap := make(map[string]bool)
			for _, p := range tt.want {
				wantMap[p] = true
			}

			for _, p := range got {
				if !wantMap[p] {
					t.Errorf("extractPorts() returned unexpected port %q, want one of %v", p, tt.want)
				}
				// Verify format is "proto:port" not "proto/port"
				hasColon := false
				for i := 0; i < len(p); i++ {
					if p[i] == ':' {
						hasColon = true
						break
					}
				}
				if !hasColon {
					t.Errorf("extractPorts() returned port %q without colon separator", p)
				}
			}
		})
	}
}

func TestTailserveReconciler_ValidateServiceName(t *testing.T) {
	r := &TailserveReconciler{}

	tests := []struct {
		name        string
		serviceName string
		wantErr     bool
	}{
		{
			name:        "valid service name",
			serviceName: "svc:web-server",
			wantErr:     false,
		},
		{
			name:        "valid with numbers",
			serviceName: "svc:app-v2",
			wantErr:     false,
		},
		{
			name:        "missing svc prefix",
			serviceName: "web-server",
			wantErr:     true,
		},
		{
			name:        "empty after prefix",
			serviceName: "svc:",
			wantErr:     true,
		},
		{
			name:        "starts with hyphen",
			serviceName: "svc:-webserver",
			wantErr:     true,
		},
		{
			name:        "ends with hyphen",
			serviceName: "svc:webserver-",
			wantErr:     true,
		},
		{
			name:        "contains invalid characters",
			serviceName: "svc:web_server",
			wantErr:     true,
		},
		{
			name:        "too long",
			serviceName: "svc:" + string(make([]byte, 64)),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.validateServiceName(tt.serviceName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateServiceName() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTailserveReconciler_NormalizeTags(t *testing.T) {
	r := &TailserveReconciler{}

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "already has prefix",
			input: []string{"tag:production", "tag:web"},
			want:  []string{"tag:production", "tag:web"},
		},
		{
			name:  "missing prefix",
			input: []string{"production", "web"},
			want:  []string{"tag:production", "tag:web"},
		},
		{
			name:  "mixed",
			input: []string{"tag:production", "web", "tag:api"},
			want:  []string{"tag:production", "tag:web", "tag:api"},
		},
		{
			name:  "empty list",
			input: []string{},
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.normalizeTags(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("normalizeTags() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("normalizeTags()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTailserveReconciler_HandleDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	now := metav1.Now()
	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-tailnet",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "example.ts.net",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth-secret",
				Namespace: "tailscale",
			},
		},
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-serve-config",
			Namespace: "tailscale",
		},
		Data: map[string]string{
			"serve-config.json": "{}",
		},
	}

	tailserve := &tailcarv1alpha1.Tailserve{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-tailserve",
			Finalizers:        []string{tailserveFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: tailcarv1alpha1.TailserveSpec{
			ServiceName: "web-server",
			TailnetRef:  "my-tailnet",
			Handlers: []tailcarv1alpha1.ServiceHandler{
				{
					Port:     443,
					Protocol: "https",
				},
			},
		},
		Status: tailcarv1alpha1.TailserveStatus{
			ConfigMapName: "test-serve-config",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(tailnet, tailserve, configMap).
		Build()

	r := &TailserveReconciler{
		Client: client,
		Scheme: scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: "test-tailserve",
		},
	}

	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	// Verify ConfigMap was deleted
	cm := &corev1.ConfigMap{}
	err = client.Get(context.Background(), types.NamespacedName{
		Name:      "test-serve-config",
		Namespace: "tailscale",
	}, cm)
	if err == nil {
		t.Error("ConfigMap should have been deleted")
	}
}
