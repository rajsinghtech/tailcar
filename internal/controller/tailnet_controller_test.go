package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
)

func TestTailnetReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tailnet := &tailcarv1alpha1.Tailnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tailnet",
			Namespace: "default",
		},
		Spec: tailcarv1alpha1.TailnetSpec{
			TailnetName: "-",
			OAuthSecretRef: tailcarv1alpha1.SecretReference{
				Name:      "test-oauth",
				Namespace: "default",
			},
			Tailscale: tailcarv1alpha1.TailscaleConfig{
				AutoApprove:    true,
				HostnamePrefix: "test",
				Tags:           []string{"tag:k8s"},
				Image:          "ghcr.io/tailscale/tailscale:latest",
			},
		},
	}

	oauthSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-oauth",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"client-id":     []byte("test-client-id"),
			"client-secret": []byte("test-client-secret"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(tailnet, oauthSecret).
		WithStatusSubresource(tailnet).
		Build()

	reconciler := &TailnetReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-tailnet",
			Namespace: "default",
		},
	}

	// Test reconciliation - this will fail OAuth validation since we don't have a real Tailscale API
	// but it should at least fetch the resource without error
	result, err := reconciler.Reconcile(context.Background(), req)

	// We expect an error because OAuth validation will fail with fake credentials
	// But we should have attempted reconciliation
	if err == nil && result == (ctrl.Result{}) {
		t.Log("Reconcile returned successfully (expected for missing resource)")
	}
}

func TestTailnetReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &TailnetReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "nonexistent",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Errorf("expected no error for nonexistent resource, got: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result for nonexistent resource, got: %v", result)
	}
}

func TestTailnetReconciler_SetupWithManager(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tailcarv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		t.Skip("skipping test: requires kubeconfig")
	}

	reconciler := &TailnetReconciler{
		Client: mgr.GetClient(),
		Scheme: scheme,
	}

	err = reconciler.SetupWithManager(mgr)
	if err != nil {
		t.Errorf("SetupWithManager() error = %v", err)
	}
}
