// Package webhook contains admission webhooks for Tailcar.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
)

const (
	// AnnotationInject enables sidecar injection when set to "true".
	AnnotationInject = "tailcar.rajsingh.info/inject"
	// AnnotationTailnet specifies the Tailnet resource to use.
	AnnotationTailnet = "tailcar.rajsingh.info/tailnet"

	// NamespaceLabelInject enables namespace-level injection when set to "enabled".
	NamespaceLabelInject = "tailcar.rajsingh.info/injection"
	// NamespaceLabelTailnet specifies the default Tailnet for the namespace.
	NamespaceLabelTailnet = "tailcar.rajsingh.info/default-tailnet"

	// TailscaleSidecarName is the name of the Tailscale sidecar container.
	TailscaleSidecarName = "tailscale"
)

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.tailcar.rajsingh.info,admissionReviewVersions=v1,sideEffects=None
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// PodMutator injects Tailscale sidecars into pods.
type PodMutator struct {
	Client  client.Client
	decoder *admission.Decoder
}

// InjectDecoder injects the decoder into the webhook.
func (m *PodMutator) InjectDecoder(d *admission.Decoder) error {
	m.decoder = d
	return nil
}

// Handle processes the admission request and mutates pods.
func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx).WithValues("namespace", req.Namespace, "pod", req.Name)

	pod := &corev1.Pod{}
	if err := m.decoder.Decode(req, pod); err != nil {
		logger.Error(err, "Failed to decode pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	namespace := &corev1.Namespace{}
	if err := m.Client.Get(ctx, types.NamespacedName{Name: req.Namespace}, namespace); err != nil {
		logger.Error(err, "Failed to get namespace")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if !shouldInject(pod, namespace) {
		logger.V(1).Info("Skipping injection - not requested")
		return admission.Allowed("injection not requested")
	}

	if isInjected(pod) {
		logger.V(1).Info("Skipping injection - already injected")
		return admission.Allowed("already injected")
	}

	tailnetName := getTailnetName(pod, namespace)
	if tailnetName == "" {
		logger.Info("No tailnet specified")
		return admission.Denied("tailnet not specified in pod annotation or namespace label")
	}

	tailnet := &tailcarv1alpha1.Tailnet{}
	if err := m.Client.Get(ctx, types.NamespacedName{Name: tailnetName}, tailnet); err != nil {
		logger.Error(err, "Failed to get Tailnet resource", "tailnet", tailnetName)
		return admission.Denied(fmt.Sprintf("tailnet %s not found: %v", tailnetName, err))
	}

	if !isTailnetReady(tailnet) {
		logger.Info("Tailnet not ready", "tailnet", tailnetName)
		return admission.Denied(fmt.Sprintf("tailnet %s is not ready", tailnetName))
	}

	// Verify auth key secret exists before injection
	authKeySecretName := fmt.Sprintf("%s-authkey", tailnet.Name)
	authKeySecret := &corev1.Secret{}
	if err := m.Client.Get(ctx, types.NamespacedName{
		Name:      authKeySecretName,
		Namespace: tailnet.Spec.OAuthSecretRef.Namespace,
	}, authKeySecret); err != nil {
		logger.Error(err, "Auth key secret not found", "secret", authKeySecretName)
		return admission.Denied(fmt.Sprintf("auth key secret %s not found: %v", authKeySecretName, err))
	}

	mutated := pod.DeepCopy()
	if err := m.injectSidecar(mutated, tailnet); err != nil {
		logger.Error(err, "Failed to inject sidecar")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	marshaledMutated, err := json.Marshal(mutated)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	source := getInjectionSource(pod, namespace)
	logger.Info("Injecting Tailscale sidecar", "tailnet", tailnetName, "source", source)
	return admission.PatchResponseFromRaw(marshaledPod, marshaledMutated)
}

func shouldInject(pod *corev1.Pod, namespace *corev1.Namespace) bool {
	if pod.Annotations != nil {
		if inject, ok := pod.Annotations[AnnotationInject]; ok {
			return inject == "true"
		}
	}

	if namespace.Labels != nil {
		if inject, ok := namespace.Labels[NamespaceLabelInject]; ok {
			return inject == "enabled"
		}
	}

	return false
}

func getTailnetName(pod *corev1.Pod, namespace *corev1.Namespace) string {
	if pod.Annotations != nil {
		if tailnet, ok := pod.Annotations[AnnotationTailnet]; ok && tailnet != "" {
			return tailnet
		}
	}

	if namespace.Labels != nil {
		if tailnet, ok := namespace.Labels[NamespaceLabelTailnet]; ok && tailnet != "" {
			return tailnet
		}
	}

	return ""
}

func getInjectionSource(pod *corev1.Pod, namespace *corev1.Namespace) string {
	if pod.Annotations != nil {
		if _, ok := pod.Annotations[AnnotationInject]; ok {
			return "pod-annotation"
		}
	}
	if namespace.Labels != nil {
		if _, ok := namespace.Labels[NamespaceLabelInject]; ok {
			return "namespace-label"
		}
	}
	return "unknown"
}

func isInjected(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == TailscaleSidecarName {
			return true
		}
	}
	return false
}

func isTailnetReady(tailnet *tailcarv1alpha1.Tailnet) bool {
	for _, condition := range tailnet.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (m *PodMutator) injectSidecar(pod *corev1.Pod, tailnet *tailcarv1alpha1.Tailnet) error {
	env := buildEnv(tailnet)
	stateDir := "/var/lib/tailscale"
	if tailnet.Spec.Tailscale.StateDir != "" {
		stateDir = tailnet.Spec.Tailscale.StateDir
	}
	sidecar := buildSidecarContainer(tailnet, env, stateDir)
	volumes := buildVolumes(tailnet)
	initContainer := buildInitContainer(tailnet)
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)
	pod.Spec.Containers = append(pod.Spec.Containers, sidecar)
	pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations["tailcar.rajsingh.info/injected"] = "true"
	pod.Annotations["tailcar.rajsingh.info/tailnet"] = tailnet.Name

	return nil
}

func buildEnv(tailnet *tailcarv1alpha1.Tailnet) []corev1.EnvVar {
	acceptDNS := "true"
	if tailnet.Spec.Tailscale.AcceptDNS != nil && !*tailnet.Spec.Tailscale.AcceptDNS {
		acceptDNS = "false"
	}

	userspace := "false"
	if tailnet.Spec.Tailscale.Userspace != nil && *tailnet.Spec.Tailscale.Userspace {
		userspace = "true"
	}

	stateDir := "/var/lib/tailscale"
	if tailnet.Spec.Tailscale.StateDir != "" {
		stateDir = tailnet.Spec.Tailscale.StateDir
	}

	env := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "POD_UID",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.uid",
				},
			},
		},
		{
			Name: "TS_AUTHKEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-authkey", tailnet.Name),
					},
					Key: "TS_AUTHKEY",
				},
			},
		},
		{
			Name:  "TS_HOSTNAME",
			Value: "$(POD_NAME)",
		},
		{
			Name:  "TS_STATE_DIR",
			Value: stateDir,
		},
		{
			Name:  "TS_ACCEPT_DNS",
			Value: acceptDNS,
		},
		{
			Name:  "TS_KUBE_SECRET",
			Value: "",
		},
		{
			Name:  "TS_USERSPACE",
			Value: userspace,
		},
		{
			Name:  "TS_DEBUG_FIREWALL_MODE",
			Value: "auto",
		},
	}

	env = append(env, tailnet.Spec.Tailscale.Env...)

	return env
}

func buildSidecarContainer(tailnet *tailcarv1alpha1.Tailnet, env []corev1.EnvVar, stateDir string) corev1.Container {
	image := tailnet.Spec.Tailscale.Image
	if image == "" {
		image = "ghcr.io/tailscale/tailscale:latest"
	}

	container := corev1.Container{
		Name:            TailscaleSidecarName,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             env,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "tailscale-state",
				MountPath: stateDir,
			},
			{
				Name:      "dev-net-tun",
				MountPath: "/dev/net/tun",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: func() *bool { b := true; return &b }(),
		},
	}

	return container
}

func buildInitContainer(tailnet *tailcarv1alpha1.Tailnet) corev1.Container {
	image := tailnet.Spec.Tailscale.Image
	if image == "" {
		image = "ghcr.io/tailscale/tailscale:latest"
	}

	privileged := true
	return corev1.Container{
		Name:    "sysctler",
		Image:   image,
		Command: []string{"/bin/sh", "-c"},
		Args: []string{
			"sysctl -w net.ipv4.ip_forward=1 && if sysctl net.ipv6.conf.all.forwarding; then sysctl -w net.ipv6.conf.all.forwarding=1; fi",
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: &privileged,
		},
	}
}

func buildVolumes(_ *tailcarv1alpha1.Tailnet) []corev1.Volume {
	charDevice := corev1.HostPathCharDev
	volumes := []corev1.Volume{
		{
			Name: "tailscale-state",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "dev-net-tun",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/dev/net/tun",
					Type: &charDevice,
				},
			},
		},
	}

	return volumes
}
