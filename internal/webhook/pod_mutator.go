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
	AnnotationInject      = "tailcar.rajsingh.info/inject"
	AnnotationTailnet     = "tailcar.rajsingh.info/tailnet"
	AnnotationTailserve   = "tailcar.rajsingh.info/tailserve"
	NamespaceLabelInject  = "tailcar.rajsingh.info/injection"
	NamespaceLabelTailnet = "tailcar.rajsingh.info/default-tailnet"
	TailscaleSidecarName  = "tailscale"
)

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.tailcar.rajsingh.info,admissionReviewVersions=v1,sideEffects=None
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=tailcar.rajsingh.info,resources=tailserves,verbs=get;list;watch

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

	var tailserveConfigMap *corev1.ConfigMap
	if tailserveName := getTailserveName(pod); tailserveName != "" {
		tailserve := &tailcarv1alpha1.Tailserve{}
		if err := m.Client.Get(ctx, types.NamespacedName{Name: tailserveName}, tailserve); err == nil {
			if tailserve.Status.ConfigMapName != "" {
				cm := &corev1.ConfigMap{}
				cmKey := types.NamespacedName{
					Name:      tailserve.Status.ConfigMapName,
					Namespace: tailnet.Spec.OAuthSecretRef.Namespace,
				}
				if err := m.Client.Get(ctx, cmKey, cm); err == nil {
					tailserveConfigMap = cm
					logger.Info("Found Tailserve config", "tailserve", tailserveName, "configMap", cm.Name)
				}
			}
		}
	}

	authKeySecretName := fmt.Sprintf("%s-authkey", tailnet.Name)
	if err := m.ensureAuthKeySecret(ctx, req.Namespace, authKeySecretName, tailnet); err != nil {
		logger.Error(err, "Failed to ensure auth key secret in namespace", "namespace", req.Namespace)
		return admission.Denied(fmt.Sprintf("failed to ensure auth key secret: %v", err))
	}

	if tailserveConfigMap != nil {
		if err := m.ensureServeConfigMap(ctx, req.Namespace, tailserveConfigMap, tailnet); err != nil {
			logger.Error(err, "Failed to ensure serve config in namespace", "namespace", req.Namespace)
			return admission.Denied(fmt.Sprintf("failed to ensure serve config: %v", err))
		}
	}

	mutated := pod.DeepCopy()
	if err := m.injectSidecar(mutated, tailnet, tailserveConfigMap); err != nil {
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

func getTailserveName(pod *corev1.Pod) string {
	if pod.Annotations != nil {
		if tailserve, ok := pod.Annotations[AnnotationTailserve]; ok && tailserve != "" {
			return tailserve
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

func (m *PodMutator) injectSidecar(pod *corev1.Pod, tailnet *tailcarv1alpha1.Tailnet, serveConfigMap *corev1.ConfigMap) error {
	tailserveName := getTailserveName(pod)
	var tailserve *tailcarv1alpha1.Tailserve
	if tailserveName != "" && serveConfigMap != nil {
		ts := &tailcarv1alpha1.Tailserve{}
		if err := m.Client.Get(context.Background(), types.NamespacedName{Name: tailserveName}, ts); err == nil {
			tailserve = ts
		}
	}

	env := buildEnv(tailnet, serveConfigMap)
	stateDir := "/var/lib/tailscale"
	if tailnet.Spec.Tailscale.StateDir != "" {
		stateDir = tailnet.Spec.Tailscale.StateDir
	}
	sidecar := buildSidecarContainer(tailnet, env, stateDir, serveConfigMap, tailserve)
	volumes := buildVolumes(tailnet, serveConfigMap)
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

func buildEnv(tailnet *tailcarv1alpha1.Tailnet, serveConfigMap *corev1.ConfigMap) []corev1.EnvVar {
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

	if serveConfigMap != nil {
		env = append(env, corev1.EnvVar{
			Name:  "TS_SERVE_CONFIG",
			Value: "/etc/tailscale/serve/serve-config.json",
		})
	}

	env = append(env, tailnet.Spec.Tailscale.Env...)

	return env
}

func buildSidecarContainer(tailnet *tailcarv1alpha1.Tailnet, env []corev1.EnvVar, stateDir string, serveConfigMap *corev1.ConfigMap, tailserve *tailcarv1alpha1.Tailserve) corev1.Container {
	image := tailnet.Spec.Tailscale.Image
	if image == "" {
		image = "ghcr.io/tailscale/tailscale:latest"
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "tailscale-state",
			MountPath: stateDir,
		},
		{
			Name:      "dev-net-tun",
			MountPath: "/dev/net/tun",
		},
	}

	if serveConfigMap != nil {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "tailscale-serve-config",
			MountPath: "/etc/tailscale/serve",
			ReadOnly:  true,
		})
	}

	container := corev1.Container{
		Name:            TailscaleSidecarName,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             env,
		VolumeMounts:    volumeMounts,
		SecurityContext: &corev1.SecurityContext{
			Privileged: func() *bool { b := true; return &b }(),
		},
	}

	if tailserve != nil {
		container.Lifecycle = buildAdvertiseLifecycleHook(tailserve)
	}

	return container
}

func buildAdvertiseLifecycleHook(tailserve *tailcarv1alpha1.Tailserve) *corev1.Lifecycle {
	serviceName := fmt.Sprintf("svc:%s", tailserve.Spec.ServiceName)
	script := fmt.Sprintf(`
#!/bin/sh
# Wait for tailscaled socket
while [ ! -S /var/run/tailscale/tailscaled.sock ] && [ ! -S /tmp/tailscaled.sock ]; do
  sleep 1
done
# Wait for Tailscale to be authenticated (up to 60 seconds)
for i in $(seq 1 60); do
  if tailscale status --json >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
# Advertise the service
tailscale serve advertise %s
`, serviceName)

	return &corev1.Lifecycle{
		PostStart: &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", script},
			},
		},
	}
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

func buildVolumes(_ *tailcarv1alpha1.Tailnet, serveConfigMap *corev1.ConfigMap) []corev1.Volume {
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

	// Add serve config volume if provided
	if serveConfigMap != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "tailscale-serve-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: serveConfigMap.Name,
					},
				},
			},
		})
	}

	return volumes
}

func (m *PodMutator) ensureServeConfigMap(ctx context.Context, namespace string, sourceConfigMap *corev1.ConfigMap, tailnet *tailcarv1alpha1.Tailnet) error {
	logger := log.FromContext(ctx).WithValues("namespace", namespace, "configMap", sourceConfigMap.Name)

	// Check if ConfigMap already exists in the pod's namespace
	configMap := &corev1.ConfigMap{}
	err := m.Client.Get(ctx, types.NamespacedName{
		Name:      sourceConfigMap.Name,
		Namespace: namespace,
	}, configMap)

	if err == nil {
		if _, ok := configMap.Data["serve-config.json"]; ok {
			logger.V(1).Info("Serve config ConfigMap already exists in namespace")
			return nil
		}
	}

	targetConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sourceConfigMap.Name,
			Namespace: namespace,
			Labels: map[string]string{
				"tailcar.rajsingh.info/tailnet": tailnet.Name,
			},
		},
		Data: sourceConfigMap.Data,
	}

	if configMap.Name != "" {
		configMap.Data = targetConfigMap.Data
		if err := m.Client.Update(ctx, configMap); err != nil {
			return fmt.Errorf("failed to update serve config ConfigMap: %w", err)
		}
		logger.Info("Updated serve config ConfigMap in namespace")
	} else {
		if err := m.Client.Create(ctx, targetConfigMap); err != nil {
			return fmt.Errorf("failed to create serve config ConfigMap: %w", err)
		}
		logger.Info("Created serve config ConfigMap in namespace")
	}

	return nil
}

func (m *PodMutator) ensureAuthKeySecret(ctx context.Context, namespace, secretName string, tailnet *tailcarv1alpha1.Tailnet) error {
	logger := log.FromContext(ctx).WithValues("namespace", namespace, "secret", secretName)

	secret := &corev1.Secret{}
	err := m.Client.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}, secret)

	if err == nil {
		if _, ok := secret.Data["TS_AUTHKEY"]; ok {
			logger.V(1).Info("Auth key secret already exists in namespace")
			return nil
		}
	}

	sourceSecret := &corev1.Secret{}
	err = m.Client.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: tailnet.Spec.OAuthSecretRef.Namespace,
	}, sourceSecret)
	if err != nil {
		return fmt.Errorf("source auth key secret not found in namespace %s: %w", tailnet.Spec.OAuthSecretRef.Namespace, err)
	}

	authKey, ok := sourceSecret.Data["TS_AUTHKEY"]
	if !ok {
		return fmt.Errorf("TS_AUTHKEY not found in source secret")
	}

	targetSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"tailcar.rajsingh.info/tailnet": tailnet.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"TS_AUTHKEY": authKey,
		},
	}

	if secret.Name != "" {
		secret.Data = targetSecret.Data
		if err := m.Client.Update(ctx, secret); err != nil {
			return fmt.Errorf("failed to update auth key secret: %w", err)
		}
		logger.Info("Updated auth key secret in namespace")
	} else {
		if err := m.Client.Create(ctx, targetSecret); err != nil {
			return fmt.Errorf("failed to create auth key secret: %w", err)
		}
		logger.Info("Created auth key secret in namespace")
	}

	return nil
}
