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

func TestPodReconciler_Reconcile_NonInjectedPod(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

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

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pod).
		Build()

	reconciler := &PodReconciler{
		Client: fakeClient,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result, got: %v", result)
	}
}

func TestPodReconciler_Reconcile_InjectedPod(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationInjected: "true",
				AnnotationTailnet:  "test-tailnet",
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

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pod).
		Build()

	reconciler := &PodReconciler{
		Client: fakeClient,
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	// Should add finalizer - this will requeue after device discovery fails
	result, err := reconciler.Reconcile(context.Background(), req)

	// Expect requeue after time due to device discovery failure (Tailnet not found)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected requeue after device discovery attempt")
	}
}

func TestPodReconciler_ReconcileNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = tailcarv1alpha1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &PodReconciler{
		Client: fakeClient,
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

func TestTagsEqual(t *testing.T) {
	tests := []struct {
		name     string
		current  []string
		desired  []string
		expected bool
	}{
		{
			name:     "equal tags",
			current:  []string{"tag:k8s", "tag:prod"},
			desired:  []string{"tag:k8s", "tag:prod"},
			expected: true,
		},
		{
			name:     "equal tags different order",
			current:  []string{"tag:prod", "tag:k8s"},
			desired:  []string{"tag:k8s", "tag:prod"},
			expected: true,
		},
		{
			name:     "different tags",
			current:  []string{"tag:k8s"},
			desired:  []string{"tag:prod"},
			expected: false,
		},
		{
			name:     "different length",
			current:  []string{"tag:k8s"},
			desired:  []string{"tag:k8s", "tag:prod"},
			expected: false,
		},
		{
			name:     "empty slices",
			current:  []string{},
			desired:  []string{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tagsEqual(tt.current, tt.desired)
			if result != tt.expected {
				t.Errorf("tagsEqual() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildExpectedHostname(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected string
	}{
		{
			name: "simple pod name",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-pod",
				},
			},
			expected: "my-pod",
		},
		{
			name: "pod with generated name",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "nginx-deployment-5869d7778c-9r28f",
				},
			},
			expected: "nginx-deployment-5869d7778c-9r28f",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildExpectedHostname(tt.pod)
			if result != tt.expected {
				t.Errorf("buildExpectedHostname() = %v, want %v", result, tt.expected)
			}
		})
	}
}
