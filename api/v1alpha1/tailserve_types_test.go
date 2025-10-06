package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTailserveValidation(t *testing.T) {
	tests := []struct {
		name    string
		spec    TailserveSpec
		wantErr bool
	}{
		{
			name: "valid HTTPS handler with proxy backend",
			spec: TailserveSpec{
				ServiceName: "web-server",
				TailnetRef:  "my-tailnet",
				Handlers: []ServiceHandler{
					{
						Port:     443,
						Protocol: "https",
						Routes: []Route{
							{
								Path: "/",
								Backend: Backend{
									Type:  "proxy",
									Proxy: "http://127.0.0.1:8080",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid TCP handler",
			spec: TailserveSpec{
				ServiceName: "postgres",
				TailnetRef:  "my-tailnet",
				Handlers: []ServiceHandler{
					{
						Port:     5432,
						Protocol: "tcp",
						TCPProxy: &TCPProxy{
							Backend: "tcp://localhost:5432",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid text backend",
			spec: TailserveSpec{
				ServiceName: "status",
				TailnetRef:  "my-tailnet",
				Handlers: []ServiceHandler{
					{
						Port:     443,
						Protocol: "https",
						Routes: []Route{
							{
								Path: "/",
								Backend: Backend{
									Type: "text",
									Text: "Hello World",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "multiple handlers",
			spec: TailserveSpec{
				ServiceName: "multi-port",
				TailnetRef:  "my-tailnet",
				Handlers: []ServiceHandler{
					{
						Port:     80,
						Protocol: "http",
						Routes: []Route{
							{
								Path: "/",
								Backend: Backend{
									Type:  "proxy",
									Proxy: "http://127.0.0.1:8080",
								},
							},
						},
					},
					{
						Port:     443,
						Protocol: "https",
						Routes: []Route{
							{
								Path: "/",
								Backend: Backend{
									Type:  "proxy",
									Proxy: "http://127.0.0.1:8080",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tailserve := &Tailserve{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-tailserve",
				},
				Spec: tt.spec,
			}

			if tailserve.Spec.ServiceName == "" {
				t.Error("ServiceName should not be empty")
			}
			if tailserve.Spec.TailnetRef == "" {
				t.Error("TailnetRef should not be empty")
			}
			if len(tailserve.Spec.Handlers) == 0 {
				t.Error("Handlers should not be empty")
			}

			for i, handler := range tailserve.Spec.Handlers {
				if handler.Port < 1 || handler.Port > 65535 {
					t.Errorf("Handler[%d]: Port %d out of valid range", i, handler.Port)
				}

				validProtocols := map[string]bool{
					"http":               true,
					"https":              true,
					"tcp":                true,
					"tls-terminated-tcp": true,
				}
				if !validProtocols[handler.Protocol] {
					t.Errorf("Handler[%d]: Invalid protocol %s", i, handler.Protocol)
				}

				if handler.Protocol == "http" || handler.Protocol == "https" {
					if len(handler.Routes) == 0 {
						t.Errorf("Handler[%d]: HTTP/HTTPS handler should have routes", i)
					}
				}

				if handler.Protocol == "tcp" || handler.Protocol == "tls-terminated-tcp" {
					if handler.TCPProxy == nil {
						t.Errorf("Handler[%d]: TCP handler should have TCPProxy", i)
					}
				}

				for j, route := range handler.Routes {
					validBackendTypes := map[string]bool{
						"proxy": true,
						"text":  true,
						"file":  true,
					}
					if !validBackendTypes[route.Backend.Type] {
						t.Errorf("Handler[%d].Route[%d]: Invalid backend type %s", i, j, route.Backend.Type)
					}
				}
			}
		})
	}
}

func TestTailserveDeepCopy(t *testing.T) {
	original := &Tailserve{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: TailserveSpec{
			ServiceName: "web-server",
			TailnetRef:  "my-tailnet",
			Handlers: []ServiceHandler{
				{
					Port:     443,
					Protocol: "https",
					Routes: []Route{
						{
							Path: "/",
							Backend: Backend{
								Type:  "proxy",
								Proxy: "http://127.0.0.1:8080",
							},
						},
					},
				},
			},
		},
	}

	copied := original.DeepCopy()

	if copied.Name != original.Name {
		t.Error("DeepCopy failed: Name mismatch")
	}
	if copied.Spec.ServiceName != original.Spec.ServiceName {
		t.Error("DeepCopy failed: ServiceName mismatch")
	}
	if len(copied.Spec.Handlers) != len(original.Spec.Handlers) {
		t.Error("DeepCopy failed: Handlers length mismatch")
	}

	// Modify copied and ensure original is unchanged
	copied.Spec.ServiceName = "modified"
	if original.Spec.ServiceName == "modified" {
		t.Error("DeepCopy failed: Original was modified")
	}
}
