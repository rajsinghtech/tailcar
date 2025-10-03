package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGroupVersion(t *testing.T) {
	if GroupVersion.Group != "tailcar.rajsingh.info" {
		t.Errorf("expected group 'tailcar.rajsingh.info', got '%s'", GroupVersion.Group)
	}
	if GroupVersion.Version != "v1alpha1" {
		t.Errorf("expected version 'v1alpha1', got '%s'", GroupVersion.Version)
	}
}

func TestSchemeBuilder(t *testing.T) {
	if SchemeBuilder == nil {
		t.Error("expected SchemeBuilder to be initialized")
	}
	if AddToScheme == nil {
		t.Error("expected AddToScheme to be initialized")
	}
}

func TestSchemeBuilderAddToScheme(t *testing.T) {
	s := runtime.NewScheme()
	err := AddToScheme(s)
	if err != nil {
		t.Errorf("AddToScheme() error = %v", err)
	}

	// Verify the scheme has our types registered
	gvk := schema.GroupVersionKind{
		Group:   "tailcar.rajsingh.info",
		Version: "v1alpha1",
		Kind:    "Tailnet",
	}

	if !s.Recognizes(gvk) {
		t.Errorf("Scheme does not recognize Tailnet type")
	}
}
