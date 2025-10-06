package controller

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
	"github.com/rajsinghtech/tailcar/internal/tailscale"
)

const (
	tailserveFinalizer = "tailcar.rajsingh.info/tailserve-finalizer"
)

type TailserveReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tailcar.rajsingh.info,resources=tailserves,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tailcar.rajsingh.info,resources=tailserves/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tailcar.rajsingh.info,resources=tailserves/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *TailserveReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	tailserve := &tailcarv1alpha1.Tailserve{}
	if err := r.Get(ctx, req.NamespacedName, tailserve); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Tailserve")
		return ctrl.Result{}, err
	}

	if !tailserve.GetDeletionTimestamp().IsZero() {
		return r.handleDeletion(ctx, tailserve)
	}

	if !controllerutil.ContainsFinalizer(tailserve, tailserveFinalizer) {
		controllerutil.AddFinalizer(tailserve, tailserveFinalizer)
		if err := r.Update(ctx, tailserve); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	tailnetKey := client.ObjectKey{Name: tailserve.Spec.TailnetRef}
	tailnet := &tailcarv1alpha1.Tailnet{}
	if err := r.Get(ctx, tailnetKey, tailnet); err != nil {
		logger.Error(err, "Failed to get referenced Tailnet")
		return r.updateStatus(ctx, tailserve, metav1.ConditionFalse, ConditionTypeReady,
			"TailnetNotFound", fmt.Sprintf("Tailnet %s not found", tailserve.Spec.TailnetRef))
	}

	clientID, clientSecret, err := r.getOAuthCredentials(ctx, tailnet.Spec.OAuthSecretRef)
	if err != nil {
		logger.Error(err, "Failed to get OAuth credentials from secret")
		return r.updateStatus(ctx, tailserve, metav1.ConditionFalse, ConditionTypeReady,
			"OAuthCredentialsRetrievalFailed", err.Error())
	}

	magicDNSSuffix, err := r.getMagicDNSSuffix(ctx, tailnet)
	if err != nil {
		logger.Error(err, "Failed to get MagicDNS suffix")
		return r.updateStatus(ctx, tailserve, metav1.ConditionFalse, ConditionTypeReady,
			"MagicDNSUnavailable", err.Error())
	}

	serviceClient := tailscale.NewServiceClient(clientID, clientSecret, tailnet.Spec.TailnetName)
	serviceName := fmt.Sprintf("svc:%s", tailserve.Spec.ServiceName)

	if err := r.validateServiceName(serviceName); err != nil {
		logger.Error(err, "Invalid service name")
		return r.updateStatus(ctx, tailserve, metav1.ConditionFalse, ConditionTypeReady,
			"InvalidServiceName", err.Error())
	}

	ports := r.extractPorts(tailserve)
	tags := r.normalizeTags(tailnet.Spec.Tailscale.Tags)
	createReq := tailscale.CreateServiceRequest{
		Name:    serviceName,
		Comment: fmt.Sprintf("Managed by tailcar: %s", tailserve.Name),
		Ports:   ports,
		Tags:    tags,
	}

	existingService, err := serviceClient.GetService(ctx, serviceName)
	if err == nil && existingService != nil && len(existingService.Addrs) > 0 {
		createReq.Addrs = existingService.Addrs
		logger.V(1).Info("Updating existing service", "serviceName", serviceName, "addrs", existingService.Addrs)
	}

	_, err = serviceClient.CreateService(ctx, serviceName, createReq)
	if err != nil {
		logger.Error(err, "Failed to create/update service via API")
		return r.updateStatus(ctx, tailserve, metav1.ConditionFalse, ConditionTypeReady,
			"ServiceCreationFailed", err.Error())
	}

	serveConfig, err := r.generateServeConfig(tailserve, magicDNSSuffix)
	if err != nil {
		logger.Error(err, "Failed to generate serve config")
		return r.updateStatus(ctx, tailserve, metav1.ConditionFalse, ConditionTypeReady,
			"ConfigGenerationFailed", err.Error())
	}

	configMapName := fmt.Sprintf("%s-serve-config", tailserve.Name)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: tailnet.Spec.OAuthSecretRef.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}
		configMap.Data["serve-config.json"] = serveConfig

		if configMap.Labels == nil {
			configMap.Labels = make(map[string]string)
		}
		configMap.Labels["tailcar.rajsingh.info/tailserve"] = tailserve.Name
		configMap.Labels["tailcar.rajsingh.info/tailnet"] = tailserve.Spec.TailnetRef

		return nil
	})

	if err != nil {
		logger.Error(err, "Failed to create/update ConfigMap")
		return r.updateStatus(ctx, tailserve, metav1.ConditionFalse, ConditionTypeReady,
			"ConfigMapUpdateFailed", err.Error())
	}

	tailserve.Status.ConfigMapName = configMapName
	now := metav1.Now()
	tailserve.Status.LastUpdated = &now

	return r.updateStatus(ctx, tailserve, metav1.ConditionTrue, ConditionTypeReady,
		ReasonReconcileSuccess, "Serve config generated successfully")
}

func (r *TailserveReconciler) handleDeletion(ctx context.Context, tailserve *tailcarv1alpha1.Tailserve) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(tailserve, tailserveFinalizer) {
		tailnet := &tailcarv1alpha1.Tailnet{}
		tailnetKey := client.ObjectKey{Name: tailserve.Spec.TailnetRef}
		if err := r.Get(ctx, tailnetKey, tailnet); err == nil {
			clientID, clientSecret, err := r.getOAuthCredentials(ctx, tailnet.Spec.OAuthSecretRef)
			if err == nil {
				serviceClient := tailscale.NewServiceClient(clientID, clientSecret, tailnet.Spec.TailnetName)
				serviceName := fmt.Sprintf("svc:%s", tailserve.Spec.ServiceName)

				if err := serviceClient.DeleteService(ctx, serviceName); err != nil {
					logger.Error(err, "Failed to delete service via API (continuing with cleanup)")
				}
			}

			if tailserve.Status.ConfigMapName != "" {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tailserve.Status.ConfigMapName,
						Namespace: tailnet.Spec.OAuthSecretRef.Namespace,
					},
				}
				if err := r.Delete(ctx, configMap); err != nil && !apierrors.IsNotFound(err) {
					logger.Error(err, "Failed to delete ConfigMap")
					return ctrl.Result{}, err
				}
			}
		}

		controllerutil.RemoveFinalizer(tailserve, tailserveFinalizer)
		if err := r.Update(ctx, tailserve); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *TailserveReconciler) generateServeConfig(tailserve *tailcarv1alpha1.Tailserve, magicDNSSuffix string) (string, error) {
	config := map[string]interface{}{
		"Services": map[string]interface{}{
			fmt.Sprintf("svc:%s", tailserve.Spec.ServiceName): r.buildServiceConfig(tailserve, magicDNSSuffix),
		},
	}

	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal serve config: %w", err)
	}

	return string(configJSON), nil
}

func (r *TailserveReconciler) buildServiceConfig(tailserve *tailcarv1alpha1.Tailserve, magicDNSSuffix string) map[string]interface{} {
	serviceConfig := map[string]interface{}{
		"TCP": make(map[string]interface{}),
		"Web": make(map[string]interface{}),
	}

	serviceFQDN := fmt.Sprintf("%s.%s", tailserve.Spec.ServiceName, magicDNSSuffix)

	for _, handler := range tailserve.Spec.Handlers {
		portStr := fmt.Sprintf("%d", handler.Port)

		switch handler.Protocol {
		case "http":
			serviceConfig["TCP"].(map[string]interface{})[portStr] = map[string]interface{}{
				"HTTP": true,
			}
			r.addWebHandlers(serviceConfig, serviceFQDN, handler)

		case "https":
			serviceConfig["TCP"].(map[string]interface{})[portStr] = map[string]interface{}{
				"HTTPS": true,
			}
			r.addWebHandlers(serviceConfig, serviceFQDN, handler)

		case "tcp":
			tcpConfig := map[string]interface{}{}
			if handler.TCPProxy != nil {
				tcpConfig["TCPForward"] = handler.TCPProxy.Backend
			}
			serviceConfig["TCP"].(map[string]interface{})[portStr] = tcpConfig

		case "tls-terminated-tcp":
			tcpConfig := map[string]interface{}{}
			if handler.TCPProxy != nil {
				tcpConfig["TCPForward"] = handler.TCPProxy.Backend
				if handler.TCPProxy.TerminateTLS {
					tcpConfig["TerminateTLS"] = serviceFQDN
				}
			}
			serviceConfig["TCP"].(map[string]interface{})[portStr] = tcpConfig
		}
	}

	return serviceConfig
}

func (r *TailserveReconciler) addWebHandlers(serviceConfig map[string]interface{}, serviceName string, handler tailcarv1alpha1.ServiceHandler) {
	webKey := fmt.Sprintf("%s:%d", serviceName, handler.Port)
	handlers := make(map[string]interface{})

	if len(handler.Routes) == 0 {
		handlers["/"] = map[string]interface{}{
			"Proxy": "http://127.0.0.1:80",
		}
	} else {
		for _, route := range handler.Routes {
			path := route.Path
			if path == "" {
				path = "/"
			}

			var handlerConfig map[string]interface{}
			switch route.Backend.Type {
			case "proxy":
				handlerConfig = map[string]interface{}{
					"Proxy": route.Backend.Proxy,
				}
			case "text":
				handlerConfig = map[string]interface{}{
					"Text": route.Backend.Text,
				}
			case "file":
				handlerConfig = map[string]interface{}{
					"Path": route.Backend.File,
				}
			}

			handlers[path] = handlerConfig
		}
	}

	serviceConfig["Web"].(map[string]interface{})[webKey] = map[string]interface{}{
		"Handlers": handlers,
	}
}

func (r *TailserveReconciler) updateStatus(ctx context.Context, tailserve *tailcarv1alpha1.Tailserve,
	status metav1.ConditionStatus, conditionType, reason, message string) (ctrl.Result, error) {

	tailserve.Status.ObservedGeneration = tailserve.Generation

	meta.SetStatusCondition(&tailserve.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: tailserve.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	if err := r.Status().Update(ctx, tailserve); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TailserveReconciler) getMagicDNSSuffix(_ context.Context, tailnet *tailcarv1alpha1.Tailnet) (string, error) {
	if tailnet.Spec.TailnetName == "" {
		return "", fmt.Errorf("tailnet name is not set for tailnet %s", tailnet.Name)
	}

	if tailnet.Spec.TailnetName == "-" {
		if tailnet.Status.MagicDNSSuffix != "" {
			return tailnet.Status.MagicDNSSuffix, nil
		}
		return "", fmt.Errorf("MagicDNS suffix not yet discovered for tailnet %s (tailnetName is '-')", tailnet.Name)
	}

	return fmt.Sprintf("%s.ts.net", tailnet.Spec.TailnetName), nil
}

func (r *TailserveReconciler) getOAuthCredentials(ctx context.Context, secretRef tailcarv1alpha1.SecretReference) (string, string, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      secretRef.Name,
		Namespace: secretRef.Namespace,
	}, secret); err != nil {
		return "", "", fmt.Errorf("failed to get secret: %w", err)
	}

	var clientID string
	for _, key := range []string{"client-id", "client_id", "oauth_client_id", "clientID"} {
		if val, ok := secret.Data[key]; ok && len(val) > 0 {
			clientID = string(val)
			break
		}
	}
	if clientID == "" {
		return "", "", fmt.Errorf("OAuth client ID not found in secret %s/%s", secretRef.Namespace, secretRef.Name)
	}

	var clientSecret string
	for _, key := range []string{"client-secret", "client_secret", "oauth_client_secret", "clientSecret"} {
		if val, ok := secret.Data[key]; ok && len(val) > 0 {
			clientSecret = string(val)
			break
		}
	}
	if clientSecret == "" {
		return "", "", fmt.Errorf("OAuth client secret not found in secret %s/%s", secretRef.Namespace, secretRef.Name)
	}

	return clientID, clientSecret, nil
}

func (r *TailserveReconciler) validateServiceName(serviceName string) error {
	bareName, hasPrefix := cutPrefix(serviceName, "svc:")
	if !hasPrefix {
		return fmt.Errorf("service name %q must start with 'svc:'", serviceName)
	}

	if bareName == "" {
		return fmt.Errorf("service name %q must not be empty after 'svc:' prefix", serviceName)
	}

	if len(bareName) > 63 {
		return fmt.Errorf("service name %q is too long (max 63 chars after 'svc:')", serviceName)
	}

	for i, c := range bareName {
		if !isAlphaNum(byte(c)) && c != '-' {
			return fmt.Errorf("service name %q contains invalid character at position %d", serviceName, i)
		}
		if i == 0 && !isAlphaNum(byte(c)) {
			return fmt.Errorf("service name %q must start with alphanumeric character", serviceName)
		}
		if i == len(bareName)-1 && c == '-' {
			return fmt.Errorf("service name %q must end with alphanumeric character", serviceName)
		}
	}

	return nil
}

func (r *TailserveReconciler) normalizeTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		_, hasPrefix := cutPrefix(tag, "tag:")
		if hasPrefix {
			normalized = append(normalized, tag)
		} else {
			normalized = append(normalized, "tag:"+tag)
		}
	}
	return normalized
}

func (r *TailserveReconciler) extractPorts(tailserve *tailcarv1alpha1.Tailserve) []string {
	portsMap := make(map[string]bool)

	for _, handler := range tailserve.Spec.Handlers {
		var protocol string
		switch handler.Protocol {
		case "http", "https":
			protocol = "tcp"
		case "tcp":
			protocol = "tcp"
		case "tls-terminated-tcp":
			protocol = "tcp"
		default:
			protocol = "tcp"
		}
		portStr := fmt.Sprintf("%s:%d", protocol, handler.Port)
		portsMap[portStr] = true
	}

	ports := make([]string, 0, len(portsMap))
	for port := range portsMap {
		ports = append(ports, port)
	}

	if len(ports) == 0 {
		ports = []string{"tcp:443"}
	}

	return ports
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func (r *TailserveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tailcarv1alpha1.Tailserve{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
