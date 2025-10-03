package webhook

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
)

func TestTailnetValidator_Handle_Valid(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	decoder := admission.NewDecoder(scheme)
	validator := &TailnetValidator{
		Client:  fakeClient,
		decoder: decoder,
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet",
			Namespace: "default",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "example.com",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth-secret",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				Image: "ghcr.io/tailscale/tailscale:latest",
				Tags:  []string{"tag:k8s", "tag:production"},
			},
		},
	}

	tailnetJSON, _ := json.Marshal(tailnet)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "default",
			Name:      "test-tailnet",
			Object: runtime.RawExtension{
				Raw: tailnetJSON,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected valid tailnet to be allowed, got: %v", resp.Result)
	}
}

func TestTailnetValidator_Handle_MissingTailnetName(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	decoder := admission.NewDecoder(scheme)
	validator := &TailnetValidator{
		Client:  fakeClient,
		decoder: decoder,
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet",
			Namespace: "default",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth-secret",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				Image: "ghcr.io/tailscale/tailscale:latest",
			},
		},
	}

	tailnetJSON, _ := json.Marshal(tailnet)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "default",
			Name:      "test-tailnet",
			Object: runtime.RawExtension{
				Raw: tailnetJSON,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected tailnet with missing tailnetName to be denied")
	}
}

func TestTailnetValidator_Handle_InvalidTags(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	decoder := admission.NewDecoder(scheme)
	validator := &TailnetValidator{
		Client:  fakeClient,
		decoder: decoder,
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet",
			Namespace: "default",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "example.com",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth-secret",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				Image: "ghcr.io/tailscale/tailscale:latest",
				Tags:  []string{"invalid-tag", "tag:valid"},
			},
		},
	}

	tailnetJSON, _ := json.Marshal(tailnet)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "default",
			Name:      "test-tailnet",
			Object: runtime.RawExtension{
				Raw: tailnetJSON,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected tailnet with invalid tags to be denied")
	}
}

func TestTailnetValidator_Handle_RelativeStateDir(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	decoder := admission.NewDecoder(scheme)
	validator := &TailnetValidator{
		Client:  fakeClient,
		decoder: decoder,
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet",
			Namespace: "default",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "example.com",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth-secret",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				Image:    "ghcr.io/tailscale/tailscale:latest",
				StateDir: "relative/path",
			},
		},
	}

	tailnetJSON, _ := json.Marshal(tailnet)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "default",
			Name:      "test-tailnet",
			Object: runtime.RawExtension{
				Raw: tailnetJSON,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected tailnet with relative stateDir to be denied")
	}
}

func TestTailnetValidator_Handle_MissingOAuthSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	decoder := admission.NewDecoder(scheme)
	validator := &TailnetValidator{
		Client:  fakeClient,
		decoder: decoder,
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet",
			Namespace: "default",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "example.com",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				Image: "ghcr.io/tailscale/tailscale:latest",
			},
		},
	}

	tailnetJSON, _ := json.Marshal(tailnet)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "default",
			Name:      "test-tailnet",
			Object: runtime.RawExtension{
				Raw: tailnetJSON,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected tailnet with missing oauth secret name to be denied")
	}
}

func TestTailnetValidator_Handle_EmptyImage(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	decoder := admission.NewDecoder(scheme)
	validator := &TailnetValidator{
		Client:  fakeClient,
		decoder: decoder,
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet",
			Namespace: "default",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "example.com",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth-secret",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				Image: "",
			},
		},
	}

	tailnetJSON, _ := json.Marshal(tailnet)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "default",
			Name:      "test-tailnet",
			Object: runtime.RawExtension{
				Raw: tailnetJSON,
			},
		},
	}

	resp := validator.Handle(context.Background(), req)
	if resp.Allowed {
		t.Error("expected tailnet with empty image to be denied")
	}
}
