package tailscale

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestServiceClient_CreateService(t *testing.T) {
	tests := []struct {
		name           string
		serviceName    string
		req            CreateServiceRequest
		serverResponse ServiceInfo
		serverStatus   int
		wantErr        bool
		errContains    string
	}{
		{
			name:        "successful creation with tcp ports",
			serviceName: "svc:test-service",
			req: CreateServiceRequest{
				Name:    "svc:test-service",
				Comment: "Test service",
				Ports:   []string{"tcp:443", "tcp:80"},
				Tags:    []string{"tag:test"},
			},
			serverResponse: ServiceInfo{
				Name:    "svc:test-service",
				Comment: "Test service",
				Ports:   []string{"tcp:443", "tcp:80"},
				Tags:    []string{"tag:test"},
				Addrs:   []netip.Addr{netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("fd7a:115c::1")},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:        "empty ports should fail",
			serviceName: "svc:test-service",
			req: CreateServiceRequest{
				Name:    "svc:test-service",
				Comment: "Test service",
				Ports:   []string{},
			},
			serverStatus: http.StatusBadRequest,
			wantErr:      true,
			errContains:  "ports cannot be empty",
		},
		{
			name:        "server error",
			serviceName: "svc:test-service",
			req: CreateServiceRequest{
				Name:  "svc:test-service",
				Ports: []string{"tcp:443"},
			},
			serverStatus: http.StatusInternalServerError,
			wantErr:      true,
			errContains:  "failed to create service",
		},
		{
			name:        "port range format",
			serviceName: "svc:test-service",
			req: CreateServiceRequest{
				Name:  "svc:test-service",
				Ports: []string{"tcp:8000-9000", "udp:53"},
			},
			serverResponse: ServiceInfo{
				Name:  "svc:test-service",
				Ports: []string{"tcp:8000-9000", "udp:53"},
				Addrs: []netip.Addr{netip.MustParseAddr("100.64.0.2")},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and path
				if r.Method != http.MethodPut {
					t.Errorf("expected PUT request, got %s", r.Method)
				}

				expectedPath := "/api/v2/tailnet/test-tailnet/services/" + tt.serviceName
				if r.URL.Path != expectedPath {
					t.Errorf("expected path %s, got %s", expectedPath, r.URL.Path)
				}

				// Verify request body
				body, _ := io.ReadAll(r.Body)
				var gotReq CreateServiceRequest
				if err := json.Unmarshal(body, &gotReq); err != nil {
					t.Errorf("failed to unmarshal request body: %v", err)
				}

				w.WriteHeader(tt.serverStatus)
				if tt.serverStatus == http.StatusOK || tt.serverStatus == http.StatusCreated {
					_ = json.NewEncoder(w).Encode(tt.serverResponse)
				}
			}))
			defer server.Close()

			client := &ServiceClient{
				httpClient: server.Client(),
				baseURL:    server.URL + "/api/v2",
				tailnet:    "test-tailnet",
			}

			result, err := client.CreateService(context.Background(), tt.serviceName, tt.req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Name != tt.serverResponse.Name {
				t.Errorf("expected name %q, got %q", tt.serverResponse.Name, result.Name)
			}
		})
	}
}

func TestServiceClient_GetService(t *testing.T) {
	tests := []struct {
		name           string
		serviceName    string
		serverResponse ServiceInfo
		serverStatus   int
		wantErr        bool
		wantNil        bool
	}{
		{
			name:        "successful get",
			serviceName: "svc:test-service",
			serverResponse: ServiceInfo{
				Name:    "svc:test-service",
				Comment: "Test service",
				Ports:   []string{"tcp:443"},
				Addrs:   []netip.Addr{netip.MustParseAddr("100.64.0.1")},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "service not found returns nil",
			serviceName:  "svc:missing-service",
			serverStatus: http.StatusNotFound,
			wantErr:      false,
			wantNil:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET request, got %s", r.Method)
				}

				w.WriteHeader(tt.serverStatus)
				if tt.serverStatus == http.StatusOK {
					_ = json.NewEncoder(w).Encode(tt.serverResponse)
				}
			}))
			defer server.Close()

			client := &ServiceClient{
				httpClient: server.Client(),
				baseURL:    server.URL + "/api/v2",
				tailnet:    "test-tailnet",
			}

			result, err := client.GetService(context.Background(), tt.serviceName)

			if tt.wantErr && err == nil {
				t.Errorf("expected error, got nil")
				return
			}

			if tt.wantNil && result != nil {
				t.Errorf("expected nil result, got %+v", result)
				return
			}

			if !tt.wantNil && !tt.wantErr && result == nil {
				t.Errorf("expected result, got nil")
			}
		})
	}
}

func TestServiceClient_DeleteService(t *testing.T) {
	tests := []struct {
		name         string
		serviceName  string
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful deletion",
			serviceName:  "svc:test-service",
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:         "not found is not error",
			serviceName:  "svc:missing-service",
			serverStatus: http.StatusNotFound,
			wantErr:      false,
		},
		{
			name:         "server error",
			serviceName:  "svc:test-service",
			serverStatus: http.StatusInternalServerError,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE request, got %s", r.Method)
				}

				w.WriteHeader(tt.serverStatus)
			}))
			defer server.Close()

			client := &ServiceClient{
				httpClient: server.Client(),
				baseURL:    server.URL + "/api/v2",
				tailnet:    "test-tailnet",
			}

			err := client.DeleteService(context.Background(), tt.serviceName)

			if tt.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}

			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestServiceClient_ListServices(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse ListServicesResponse
		serverStatus   int
		wantErr        bool
		wantCount      int
	}{
		{
			name: "successful list",
			serverResponse: ListServicesResponse{
				VIPServices: []ServiceInfo{
					{Name: "svc:service1", Ports: []string{"tcp:443"}},
					{Name: "svc:service2", Ports: []string{"tcp:80"}},
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
			wantCount:    2,
		},
		{
			name: "empty list",
			serverResponse: ListServicesResponse{
				VIPServices: []ServiceInfo{},
			},
			serverStatus: http.StatusOK,
			wantErr:      false,
			wantCount:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("expected GET request, got %s", r.Method)
				}

				w.WriteHeader(tt.serverStatus)
				_ = json.NewEncoder(w).Encode(tt.serverResponse)
			}))
			defer server.Close()

			client := &ServiceClient{
				httpClient: server.Client(),
				baseURL:    server.URL + "/api/v2",
				tailnet:    "test-tailnet",
			}

			result, err := client.ListServices(context.Background())

			if tt.wantErr && err == nil {
				t.Errorf("expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(result) != tt.wantCount {
				t.Errorf("expected %d services, got %d", tt.wantCount, len(result))
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
