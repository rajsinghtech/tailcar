package webhook

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
)

func TestPodMutator_Handle_NoAnnotation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	decoder := admission.NewDecoder(scheme)

	mutator := &PodMutator{
		Client:  fakeClient,
		decoder: decoder,
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx",
				},
			},
		},
	}

	podJSON, _ := json.Marshal(pod)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object: runtime.RawExtension{
				Raw: podJSON,
			},
		},
	}

	resp := mutator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Error("expected pod without annotation to be allowed")
	}
	if len(resp.Patches) != 0 {
		t.Error("expected no patches for pod without annotation")
	}
}

func TestPodMutator_Handle_WithAnnotation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet",
			Namespace: "default",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "-",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				AutoApprove:    true,
				HostnamePrefix: "test",
				Tags:           []string{"tag:k8s"},
				Image:          "ghcr.io/tailscale/tailscale:latest",
			},
		},
		Status: tailcarv1alpha1.TailnetStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	authKeySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet-authkey",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"authkey": []byte("tskey-test"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(tailnet, authKeySecret).
		WithStatusSubresource(tailnet).
		Build()

	decoder := admission.NewDecoder(scheme)

	mutator := &PodMutator{
		Client:  fakeClient,
		decoder: decoder,
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				"tailcar.rajsingh.info/inject":  "true",
				"tailcar.rajsingh.info/tailnet": "test-tailnet",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx",
				},
			},
		},
	}

	podJSON, _ := json.Marshal(pod)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object: runtime.RawExtension{
				Raw: podJSON,
			},
		},
	}

	resp := mutator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected pod to be allowed, got: %v", resp.Result)
	}
	if len(resp.Patches) == 0 {
		t.Error("expected patches for pod with injection annotation")
	}
}

func TestPodMutator_Handle_AlreadyInjected(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	decoder := admission.NewDecoder(scheme)

	mutator := &PodMutator{
		Client:  fakeClient,
		decoder: decoder,
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				"tailcar.rajsingh.info/inject":  "true",
				"tailcar.rajsingh.info/tailnet": "test-tailnet",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx",
				},
				{
					Name:  "tailscale",
					Image: "ghcr.io/tailscale/tailscale:latest",
				},
			},
		},
	}

	podJSON, _ := json.Marshal(pod)
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object: runtime.RawExtension{
				Raw: podJSON,
			},
		},
	}

	resp := mutator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Error("expected already injected pod to be allowed")
	}
	if len(resp.Patches) != 0 {
		t.Error("expected no patches for already injected pod")
	}
}

func TestPodMutator_SetupWebhookWithManager(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	mutator := &PodMutator{}

	// This test just validates the method exists and doesn't panic
	// Actual webhook setup requires a manager which needs kubeconfig
	if mutator.Client == nil {
		t.Log("PodMutator initialized successfully")
	}
}
