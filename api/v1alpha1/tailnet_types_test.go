package v1alpha1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTailnetSpec(t *testing.T) {
	spec := TailnetSpec{
		TailnetName: "test-tailnet",
		OAuthSecretRef: SecretReference{
			Name:      "oauth-secret",
			Namespace: "default",
		},
		Tailscale: TailscaleConfig{
			AutoApprove:    true,
			HostnamePrefix: "test",
			Tags:           []string{"tag:test"},
			Image:          "ghcr.io/tailscale/tailscale:latest",
		},
	}

	if spec.TailnetName != "test-tailnet" {
		t.Errorf("expected TailnetName 'test-tailnet', got '%s'", spec.TailnetName)
	}
	if spec.OAuthSecretRef.Name != "oauth-secret" {
		t.Errorf("expected OAuthSecretRef.Name 'oauth-secret', got '%s'", spec.OAuthSecretRef.Name)
	}
	if !spec.Tailscale.AutoApprove {
		t.Error("expected AutoApprove to be true")
	}
	if len(spec.Tailscale.Tags) != 1 || spec.Tailscale.Tags[0] != "tag:test" {
		t.Errorf("expected Tags ['tag:test'], got %v", spec.Tailscale.Tags)
	}
}

func TestTailnetStatus(t *testing.T) {
	status := TailnetStatus{
		ObservedGeneration: 1,
		InjectedPods:       3,
		Conditions: []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "AllReady",
				Message:            "All components ready",
			},
		},
	}

	if status.ObservedGeneration != 1 {
		t.Errorf("expected ObservedGeneration 1, got %d", status.ObservedGeneration)
	}
	if status.InjectedPods != 3 {
		t.Errorf("expected InjectedPods 3, got %d", status.InjectedPods)
	}
	if len(status.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(status.Conditions))
	}
	if status.Conditions[0].Type != "Ready" {
		t.Errorf("expected condition type 'Ready', got '%s'", status.Conditions[0].Type)
	}
}

func TestTailscaleConfig(t *testing.T) {
	config := TailscaleConfig{
		AutoApprove:    true,
		HostnamePrefix: "k8s",
		Tags:           []string{"tag:k8s", "tag:prod"},
		Image:          "ghcr.io/tailscale/tailscale:v1.50.0",
		Env: []corev1.EnvVar{
			{
				Name:  "TS_EXTRA_ARGS",
				Value: "--advertise-exit-node",
			},
		},
	}

	if !config.AutoApprove {
		t.Error("expected AutoApprove to be true")
	}
	if config.HostnamePrefix != "k8s" {
		t.Errorf("expected HostnamePrefix 'k8s', got '%s'", config.HostnamePrefix)
	}
	if len(config.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(config.Tags))
	}
	if len(config.Env) != 1 {
		t.Errorf("expected 1 env var, got %d", len(config.Env))
	}
}

func TestTailnetDefaults(t *testing.T) {
	tailnet := &Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: TailnetSpec{
			TailnetName: "-",
			OAuthSecretRef: SecretReference{
				Name:      "oauth",
				Namespace: "default",
			},
		},
	}

	if tailnet.Spec.TailnetName != "-" {
		t.Errorf("expected TailnetName '-', got '%s'", tailnet.Spec.TailnetName)
	}
}
