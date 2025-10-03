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
	// Annotations
	AnnotationInject  = "tailcar.rajsingh.info/inject"
	AnnotationTailnet = "tailcar.rajsingh.info/tailnet"

	// Sidecar container name
	TailscaleSidecarName = "tailscale"
)

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.tailcar.rajsingh.info,admissionReviewVersions=v1,sideEffects=None

type PodMutator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func (m *PodMutator) InjectDecoder(d *admission.Decoder) error {
	m.decoder = d
	return nil
}

func (m *PodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx).WithValues("namespace", req.Namespace, "pod", req.Name)

	pod := &corev1.Pod{}
	if err := m.decoder.Decode(req, pod); err != nil {
		logger.Error(err, "Failed to decode pod")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if !shouldInject(pod) {
		logger.V(1).Info("Skipping injection - not requested")
		return admission.Allowed("injection not requested")
	}

	if isInjected(pod) {
		logger.V(1).Info("Skipping injection - already injected")
		return admission.Allowed("already injected")
	}

	tailnetName := pod.Annotations[AnnotationTailnet]
	if tailnetName == "" {
		logger.Info("No tailnet specified in annotation")
		return admission.Denied("tailnet annotation required")
	}

	tailnet := &tailcarv1alpha1.Tailnet{}
	if err := m.Client.Get(ctx, types.NamespacedName{
		Name:      tailnetName,
		Namespace: pod.Namespace,
	}, tailnet); err != nil {
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
		Namespace: pod.Namespace,
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

	logger.Info("Injecting Tailscale sidecar", "tailnet", tailnetName)
	return admission.PatchResponseFromRaw(marshaledPod, marshaledMutated)
}

func shouldInject(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}

	inject, ok := pod.Annotations[AnnotationInject]
	if !ok {
		return false
	}

	return inject == "true"
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
	hostname := buildHostname(pod, tailnet)
	env := buildEnv(pod, tailnet, hostname)
	sidecar := buildSidecarContainer(tailnet, env)
	volumes := buildVolumes(tailnet)
	initContainer := buildInitContainer(tailnet)
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)
	pod.Spec.Containers = append(pod.Spec.Containers, sidecar)
	pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations["tailcar.rajsingh.info/injected"] = "true"

	return nil
}

func buildHostname(pod *corev1.Pod, tailnet *tailcarv1alpha1.Tailnet) string {
	prefix := tailnet.Spec.Tailscale.HostnamePrefix

	if prefix != "" {
		return fmt.Sprintf("%s-%s", prefix, pod.Name)
	}

	return pod.Name
}

func buildEnv(pod *corev1.Pod, tailnet *tailcarv1alpha1.Tailnet, hostname string) []corev1.EnvVar {
	env := []corev1.EnvVar{
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
			Value: hostname,
		},
		{
			Name:  "TS_STATE_DIR",
			Value: "/var/lib/tailscale",
		},
		{
			Name:  "TS_ACCEPT_DNS",
			Value: "true",
		},
		{
			Name:  "TS_ACCEPT_ROUTES",
			Value: "true",
		},
		{
			Name:  "TS_KUBE_SECRET",
			Value: "",
		},
		{
			Name:  "TS_USERSPACE",
			Value: "false",
		},
		{
			Name:  "TS_DEBUG_FIREWALL_MODE",
			Value: "auto",
		},
	}

	env = append(env, tailnet.Spec.Tailscale.Env...)

	return env
}

func buildSidecarContainer(tailnet *tailcarv1alpha1.Tailnet, env []corev1.EnvVar) corev1.Container {
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
				MountPath: "/var/lib/tailscale",
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

func buildVolumes(tailnet *tailcarv1alpha1.Tailnet) []corev1.Volume {
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
