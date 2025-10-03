package webhook

import (
	"context"
	"fmt"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
)

// +kubebuilder:webhook:path=/validate-tailcar-rajsingh-info-v1alpha1-tailnet,mutating=false,failurePolicy=fail,groups=tailcar.rajsingh.info,resources=tailnets,verbs=create;update,versions=v1alpha1,name=vtailnet.tailcar.rajsingh.info,admissionReviewVersions=v1,sideEffects=None

// TailnetValidator validates Tailnet resources.
type TailnetValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

// InjectDecoder injects the decoder into the webhook.
func (v *TailnetValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// Handle processes the validation request.
func (v *TailnetValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx).WithValues("namespace", req.Namespace, "name", req.Name)

	tailnet := &tailcarv1alpha1.Tailnet{}
	if err := v.decoder.Decode(req, tailnet); err != nil {
		logger.Error(err, "Failed to decode Tailnet")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := v.validateTailnet(tailnet); err != nil {
		logger.Info("Tailnet validation failed", "error", err.Error())
		return admission.Denied(err.Error())
	}

	return admission.Allowed("Tailnet validation passed")
}

func (v *TailnetValidator) validateTailnet(tailnet *tailcarv1alpha1.Tailnet) error {
	if tailnet.Spec.TailnetName == "" {
		return fmt.Errorf("tailnetName is required")
	}

	if tailnet.Spec.OAuthSecretRef.Name == "" {
		return fmt.Errorf("oauthSecretRef.name is required")
	}

	if tailnet.Spec.OAuthSecretRef.Namespace == "" {
		return fmt.Errorf("oauthSecretRef.namespace is required")
	}

	if tailnet.Spec.Tailscale.Image == "" {
		return fmt.Errorf("tailscale.image cannot be empty")
	}

	if tailnet.Spec.Tailscale.StateDir != "" && tailnet.Spec.Tailscale.StateDir[0] != '/' {
		return fmt.Errorf("tailscale.stateDir must be an absolute path")
	}

	for i, tag := range tailnet.Spec.Tailscale.Tags {
		if len(tag) < 4 || tag[:4] != "tag:" {
			return fmt.Errorf("tailscale.tags[%d] must start with 'tag:' (got: %s)", i, tag)
		}
	}

	return nil
}

var _ admission.Handler = &TailnetValidator{}
