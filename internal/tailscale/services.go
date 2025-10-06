package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"

	"golang.org/x/oauth2/clientcredentials"
)

type ServiceClient struct {
	httpClient *http.Client
	baseURL    string
	tailnet    string
}

func NewServiceClient(clientID, clientSecret, tailnet string) *ServiceClient {
	baseURL := "https://api.tailscale.com"

	oauthConfig := clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     baseURL + "/api/v2/oauth/token",
	}

	return &ServiceClient{
		httpClient: oauthConfig.Client(context.Background()),
		baseURL:    baseURL + "/api/v2",
		tailnet:    tailnet,
	}
}

type ServiceInfo struct {
	Name        string            `json:"name,omitempty"`
	Addrs       []netip.Addr      `json:"addrs,omitempty"`
	Comment     string            `json:"comment,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Ports       []string          `json:"ports,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
}

type CreateServiceRequest struct {
	Name        string            `json:"name,omitempty"`
	Addrs       []netip.Addr      `json:"addrs,omitempty"`
	Comment     string            `json:"comment,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Ports       []string          `json:"ports,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
}

type ListServicesResponse struct {
	VIPServices []ServiceInfo `json:"vipServices"`
}

func (c *ServiceClient) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var bodyReader *bytes.Buffer
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonData)
	}

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}

func (c *ServiceClient) CreateService(ctx context.Context, serviceName string, req CreateServiceRequest) (*ServiceInfo, error) {
	if len(req.Ports) == 0 {
		return nil, fmt.Errorf("ports cannot be empty, must provide at least one port")
	}

	path := fmt.Sprintf("/tailnet/%s/services/%s", c.tailnet, serviceName)

	resp, err := c.doRequest(ctx, http.MethodPut, path, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body := make([]byte, 1024)
		_, _ = resp.Body.Read(body)
		return nil, fmt.Errorf("failed to create service: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result ServiceInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (c *ServiceClient) GetService(ctx context.Context, serviceName string) (*ServiceInfo, error) {
	path := fmt.Sprintf("/tailnet/%s/services/%s", c.tailnet, serviceName)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get service: status %d", resp.StatusCode)
	}

	var result ServiceInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (c *ServiceClient) DeleteService(ctx context.Context, serviceName string) error {
	path := fmt.Sprintf("/tailnet/%s/services/%s", c.tailnet, serviceName)

	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete service: status %d", resp.StatusCode)
	}

	return nil
}

func (c *ServiceClient) ListServices(ctx context.Context) ([]ServiceInfo, error) {
	path := fmt.Sprintf("/tailnet/%s/services", c.tailnet)

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list services: status %d", resp.StatusCode)
	}

	var result ListServicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.VIPServices, nil
}
