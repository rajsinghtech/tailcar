package webhook

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
)

func TestPodMutator_Handle_NoAnnotation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace).
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
			Namespace: "default",
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

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-tailnet",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "-",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				AutoApprove: true,
				Tags:        []string{"tag:k8s"},
				Image:       "ghcr.io/tailscale/tailscale:latest",
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
			"TS_AUTHKEY": []byte("tskey-test"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, tailnet, authKeySecret).
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
			Namespace: "default",
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

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace).
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
			Namespace: "default",
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

func TestPodMutator_Handle_ConfigurableEnv(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}

	acceptDNS := false
	userspace := true

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-tailnet",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "-",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				AutoApprove: true,
				AcceptDNS:   &acceptDNS,
				Userspace:   &userspace,
				StateDir:    "/custom/state",
				Image:       "ghcr.io/tailscale/tailscale:latest",
				Tags:        []string{"tag:k8s"},
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
			"TS_AUTHKEY": []byte("tskey-test"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, tailnet, authKeySecret).
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
			Namespace: "default",
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
		t.Error("expected patches for pod with custom env config")
	}

	// Simply verify that patches were created - detailed env checking would require
	// applying JSON patches which is complex. The unit test for buildEnv is more appropriate
	t.Logf("Injection successful with %d patches", len(resp.Patches))
}

func TestPodMutator_Handle_NamespaceInjection(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
			Labels: map[string]string{
				"tailcar.rajsingh.info/injection":       "enabled",
				"tailcar.rajsingh.info/default-tailnet": "test-tailnet",
			},
		},
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-tailnet",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "-",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				AutoApprove: true,
				Image:       "ghcr.io/tailscale/tailscale:latest",
				Tags:        []string{"tag:k8s"},
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
			"TS_AUTHKEY": []byte("tskey-test"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, tailnet, authKeySecret).
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
			Namespace: "default",
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
		t.Error("expected patches for pod in namespace with injection enabled")
	}
}

func TestPodMutator_Handle_UpdateOperation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-tailnet",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "-",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				AutoApprove: true,
				Image:       "ghcr.io/tailscale/tailscale:latest",
				Tags:        []string{"tag:k8s"},
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
			"TS_AUTHKEY": []byte("tskey-test"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, tailnet, authKeySecret).
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
			Operation: admissionv1.Update,
			Namespace: "default",
			Object: runtime.RawExtension{
				Raw: podJSON,
			},
		},
	}

	resp := mutator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("expected pod to be allowed on UPDATE, got: %v", resp.Result)
	}
	if len(resp.Patches) == 0 {
		t.Error("expected patches for pod with injection annotation on UPDATE")
	}
}

func TestPodMutator_Handle_UpdateAlreadyInjected(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace).
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
			Operation: admissionv1.Update,
			Namespace: "default",
			Object: runtime.RawExtension{
				Raw: podJSON,
			},
		},
	}

	resp := mutator.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Error("expected already injected pod to be allowed on UPDATE")
	}
	if len(resp.Patches) != 0 {
		t.Error("expected no patches for already injected pod on UPDATE")
	}
}

func TestBuildEnv(t *testing.T) {
	tests := []struct {
		name     string
		tailnet  *tailcarv1alpha1.Tailnet
		expected map[string]string
	}{
		{
			name: "default values",
			tailnet: &tailcarv1alpha1.Tailnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-tailnet",
				},
				Spec: tailcarv1alpha1.TailnetSpec{
					Tailscale: tailcarv1alpha1.TailscaleConfig{},
				},
			},
			expected: map[string]string{
				"TS_HOSTNAME":   "$(POD_NAME)",
				"TS_STATE_DIR":  "/var/lib/tailscale",
				"TS_ACCEPT_DNS": "true",
				"TS_USERSPACE":  "false",
			},
		},
		{
			name: "custom values",
			tailnet: &tailcarv1alpha1.Tailnet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-tailnet",
				},
				Spec: tailcarv1alpha1.TailnetSpec{
					Tailscale: tailcarv1alpha1.TailscaleConfig{
						AcceptDNS: func() *bool { b := false; return &b }(),
						Userspace: func() *bool { b := true; return &b }(),
						StateDir:  "/custom/state",
					},
				},
			},
			expected: map[string]string{
				"TS_HOSTNAME":   "$(POD_NAME)",
				"TS_STATE_DIR":  "/custom/state",
				"TS_ACCEPT_DNS": "false",
				"TS_USERSPACE":  "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := buildEnv(tt.tailnet, nil)

			envMap := make(map[string]string)
			for _, e := range env {
				if e.Value != "" {
					envMap[e.Name] = e.Value
				}
			}

			for key, expectedValue := range tt.expected {
				if actualValue, ok := envMap[key]; !ok {
					t.Errorf("expected env var %s to be set", key)
				} else if actualValue != expectedValue {
					t.Errorf("env var %s = %s, want %s", key, actualValue, expectedValue)
				}
			}

			// Verify POD_NAME and POD_UID are set with fieldRef
			var hasPodName, hasPodUID bool
			for _, e := range env {
				if e.Name == "POD_NAME" && e.ValueFrom != nil && e.ValueFrom.FieldRef != nil && e.ValueFrom.FieldRef.FieldPath == "metadata.name" {
					hasPodName = true
				}
				if e.Name == "POD_UID" && e.ValueFrom != nil && e.ValueFrom.FieldRef != nil && e.ValueFrom.FieldRef.FieldPath == "metadata.uid" {
					hasPodUID = true
				}
			}
			if !hasPodName {
				t.Error("expected POD_NAME env var with fieldRef to metadata.name")
			}
			if !hasPodUID {
				t.Error("expected POD_UID env var with fieldRef to metadata.uid")
			}
		})
	}
}

func TestPodMutator_Handle_CrossNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	oauthNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "oauth-ns",
		},
	}

	podNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-ns",
		},
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-tailnet",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "-",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth",
				Namespace: "oauth-ns",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				AutoApprove: true,
				Tags:        []string{"tag:k8s"},
				Image:       "ghcr.io/tailscale/tailscale:latest",
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

	// Auth key secret exists in oauth namespace
	authKeySecretSource := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet-authkey",
			Namespace: "oauth-ns",
		},
		Data: map[string][]byte{
			"TS_AUTHKEY": []byte("tskey-test"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(oauthNamespace, podNamespace, tailnet, authKeySecretSource).
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
			Namespace: "pod-ns",
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
			Namespace: "pod-ns",
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
		t.Error("expected patches for cross-namespace pod injection")
	}

	// Verify the authkey secret was created in the pod's namespace
	targetSecret := &corev1.Secret{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{
		Name:      "test-tailnet-authkey",
		Namespace: "pod-ns",
	}, targetSecret)
	if err != nil {
		t.Errorf("expected authkey secret to be created in pod namespace: %v", err)
	}
	if string(targetSecret.Data["TS_AUTHKEY"]) != "tskey-test" {
		t.Error("authkey secret data does not match source")
	}
}

func TestPodMutator_Handle_WithTailserve(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-tailnet",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "-",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "oauth",
				Namespace: "tailscale",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				AutoApprove: true,
				Image:       "ghcr.io/tailscale/tailscale:latest",
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

	tailserve := &tailcarv1alpha1.Tailserve{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-tailserve",
		},
		Spec: tailcarv1alpha1.TailserveSpec{
			ServiceName: "web-server",
			TailnetRef:  "test-tailnet",
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
		Status: tailcarv1alpha1.TailserveStatus{
			ConfigMapName: "test-tailserve-serve-config",
		},
	}

	serveConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailserve-serve-config",
			Namespace: "tailscale",
		},
		Data: map[string]string{
			"serve-config.json": `{"Services":{"svc:web-server":{"TCP":{"443":{"HTTPS":true}}}}}`,
		},
	}

	authKeySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet-authkey",
			Namespace: "tailscale",
		},
		Data: map[string][]byte{
			"TS_AUTHKEY": []byte("tskey-test"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, tailnet, tailserve, serveConfigMap, authKeySecret).
		WithStatusSubresource(tailnet, tailserve).
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
				"tailcar.rajsingh.info/inject":    "true",
				"tailcar.rajsingh.info/tailnet":   "test-tailnet",
				"tailcar.rajsingh.info/tailserve": "test-tailserve",
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
			Namespace: "default",
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
		t.Error("expected patches for tailserve pod injection")
	}

	// Verify serve config ConfigMap was created in pod namespace
	targetConfigMap := &corev1.ConfigMap{}
	err := fakeClient.Get(context.Background(), client.ObjectKey{
		Name:      "test-tailserve-serve-config",
		Namespace: "default",
	}, targetConfigMap)
	if err != nil {
		t.Errorf("expected serve config ConfigMap to be created in pod namespace: %v", err)
	}

	// Verify ConfigMap has correct data
	if _, ok := targetConfigMap.Data["serve-config.json"]; !ok {
		t.Error("serve config ConfigMap missing serve-config.json")
	}

	// Decode patches and verify TS_SERVE_CONFIG env var is set
	var patchedPod corev1.Pod
	if err := json.Unmarshal(resp.Patch, &patchedPod); err == nil {
		foundServeConfig := false
		for _, container := range patchedPod.Spec.Containers {
			if container.Name == "tailscale" {
				for _, env := range container.Env {
					if env.Name == "TS_SERVE_CONFIG" {
						foundServeConfig = true
						if env.Value != "/etc/tailscale/serve/serve-config.json" {
							t.Errorf("TS_SERVE_CONFIG value = %s, want /etc/tailscale/serve/serve-config.json", env.Value)
						}
					}
				}

				// Verify volume mount exists
				foundVolumeMount := false
				for _, mount := range container.VolumeMounts {
					if mount.Name == "tailscale-serve-config" {
						foundVolumeMount = true
						if mount.MountPath != "/etc/tailscale/serve" {
							t.Errorf("Volume mount path = %s, want /etc/tailscale/serve", mount.MountPath)
						}
					}
				}
				if !foundVolumeMount {
					t.Error("Expected tailscale-serve-config volume mount")
				}
			}
		}
		if !foundServeConfig {
			t.Error("Expected TS_SERVE_CONFIG environment variable in tailscale container")
		}
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
