// Package controller contains Kubernetes controllers for the Tailcar operator.
package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
	"tailscale.com/client/tailscale/v2"
)

const (
	// PodFinalizerName is the finalizer added to pods for device cleanup.
	PodFinalizerName = "tailcar.rajsingh.info/device-cleanup"
	// AnnotationDeviceID stores the Tailscale device ID.
	AnnotationDeviceID = "tailcar.rajsingh.info/device-id"
	// AnnotationInjected marks a pod as having been injected.
	AnnotationInjected = "tailcar.rajsingh.info/injected"
	// AnnotationTailnet references the Tailnet resource name.
	AnnotationTailnet = "tailcar.rajsingh.info/tailnet"
	// DeviceCleanupRequeueTime is the time to wait before retrying device cleanup.
	DeviceCleanupRequeueTime = 30 * time.Second
)

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=pods/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=tailcar.rajsingh.info,resources=tailnets,verbs=get;list;watch

// PodReconciler reconciles Pod objects with Tailscale device cleanup.
type PodReconciler struct {
	client.Client
}

// Reconcile handles pod reconciliation for Tailscale device cleanup.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Pod")
		return ctrl.Result{}, err
	}

	if pod.Annotations[AnnotationInjected] != "true" {
		return ctrl.Result{}, nil
	}

	if !pod.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, pod)
	}

	if !controllerutil.ContainsFinalizer(pod, PodFinalizerName) {
		controllerutil.AddFinalizer(pod, PodFinalizerName)
		if err := r.Update(ctx, pod); err != nil {
			if errors.IsConflict(err) {
				logger.V(1).Info("Conflict adding finalizer, will retry")
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("Added finalizer to pod")
	}

	deviceID := pod.Annotations[AnnotationDeviceID]
	if deviceID == "" {
		if err := r.discoverDeviceID(ctx, pod); err != nil {
			logger.Info("Device not yet registered, will retry", "error", err.Error())
			return ctrl.Result{RequeueAfter: DeviceCleanupRequeueTime}, nil
		}
		return ctrl.Result{}, nil
	}

	if err := r.reconcileDeviceTags(ctx, pod, deviceID); err != nil {
		logger.Info("Failed to reconcile device tags, will retry", "error", err.Error())
		return ctrl.Result{RequeueAfter: DeviceCleanupRequeueTime}, nil
	}

	return ctrl.Result{}, nil
}

func (r *PodReconciler) handleDeletion(ctx context.Context, pod *corev1.Pod) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(pod, PodFinalizerName) {
		return ctrl.Result{}, nil
	}

	tailnetName := pod.Annotations[AnnotationTailnet]
	deviceID := pod.Annotations[AnnotationDeviceID]
	if deviceID != "" {
		if tailnetName == "" {
			logger.Info("No tailnet annotation found, skipping device cleanup")
		} else {
			if err := r.cleanupDevice(ctx, pod.Namespace, tailnetName, deviceID); err != nil {
				logger.Error(err, "Failed to cleanup device, will retry")
				return ctrl.Result{RequeueAfter: DeviceCleanupRequeueTime}, err
			}
			logger.Info("Successfully deleted device from Tailscale", "deviceID", deviceID)
		}
	}

	// Decrement InjectedPods counter
	if tailnetName != "" {
		if err := r.decrementInjectedPods(ctx, pod.Namespace, tailnetName); err != nil {
			logger.Error(err, "Failed to decrement InjectedPods counter")
			// Don't fail the deletion due to counter update failure
		}
	}

	controllerutil.RemoveFinalizer(pod, PodFinalizerName)
	if err := r.Update(ctx, pod); err != nil {
		if errors.IsConflict(err) {
			logger.V(1).Info("Conflict removing finalizer, will retry")
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PodReconciler) cleanupDevice(ctx context.Context, namespace, tailnetName, deviceID string) error {
	logger := log.FromContext(ctx)

	tailnet := &tailcarv1alpha1.Tailnet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      tailnetName,
		Namespace: namespace,
	}, tailnet); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Tailnet not found, skipping device cleanup", "tailnet", tailnetName)
			return nil
		}
		return fmt.Errorf("failed to get Tailnet: %w", err)
	}

	tsClient, err := r.getTailscaleClient(ctx, tailnet)
	if err != nil {
		return fmt.Errorf("failed to create Tailscale client: %w", err)
	}

	if err := tsClient.Devices().Delete(ctx, deviceID); err != nil {
		errMsg := err.Error()
		// Check if device is already gone (404 error or not found)
		if errors.IsNotFound(err) || strings.Contains(errMsg, "404") || strings.Contains(errMsg, "not found") {
			logger.Info("Device already deleted", "deviceID", deviceID)
			return nil
		}
		// Don't retry on permission errors
		if strings.Contains(errMsg, "403") || strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "forbidden") {
			return fmt.Errorf("permission denied deleting device: %w", err)
		}
		// All other errors (network timeouts, etc.) should be retried
		return fmt.Errorf("failed to delete device: %w", err)
	}

	return nil
}

func (r *PodReconciler) discoverDeviceID(ctx context.Context, pod *corev1.Pod) error {
	logger := log.FromContext(ctx)

	tailnetName := pod.Annotations[AnnotationTailnet]
	if tailnetName == "" {
		return nil
	}

	tailnet := &tailcarv1alpha1.Tailnet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      tailnetName,
		Namespace: pod.Namespace,
	}, tailnet); err != nil {
		return fmt.Errorf("failed to get Tailnet: %w", err)
	}

	tsClient, err := r.getTailscaleClient(ctx, tailnet)
	if err != nil {
		return fmt.Errorf("failed to create Tailscale client: %w", err)
	}

	expectedHostname := buildExpectedHostname(pod)

	// Search for device by hostname
	// Note: We iterate through all devices as Tailscale API doesn't support hostname filtering
	devices, err := tsClient.Devices().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list devices: %w", err)
	}

	for _, device := range devices {
		if device.Hostname == expectedHostname {
			if pod.Annotations == nil {
				pod.Annotations = make(map[string]string)
			}
			pod.Annotations[AnnotationDeviceID] = device.NodeID
			if err := r.Update(ctx, pod); err != nil {
				if errors.IsConflict(err) {
					logger.V(1).Info("Conflict updating device ID, will retry")
					return fmt.Errorf("conflict updating pod: %w", err)
				}
				return fmt.Errorf("failed to update pod with device ID: %w", err)
			}
			logger.Info("Discovered and stored device ID", "deviceID", device.NodeID, "hostname", expectedHostname)
			return nil
		}
	}

	return fmt.Errorf("device not found for hostname: %s", expectedHostname)
}

func (r *PodReconciler) getTailscaleClient(ctx context.Context, tailnet *tailcarv1alpha1.Tailnet) (*tailscale.Client, error) {
	oauthSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      tailnet.Spec.OAuthSecretRef.Name,
		Namespace: tailnet.Spec.OAuthSecretRef.Namespace,
	}, oauthSecret); err != nil {
		return nil, fmt.Errorf("failed to get OAuth secret: %w", err)
	}

	clientIDBytes, ok := oauthSecret.Data["client-id"]
	if !ok || len(clientIDBytes) == 0 {
		return nil, fmt.Errorf("client-id missing or empty in OAuth secret")
	}

	clientSecretBytes, ok := oauthSecret.Data["client-secret"]
	if !ok || len(clientSecretBytes) == 0 {
		return nil, fmt.Errorf("client-secret missing or empty in OAuth secret")
	}

	clientID := string(clientIDBytes)
	clientSecret := string(clientSecretBytes)

	tsClient := &tailscale.Client{
		Tailnet: tailnet.Spec.TailnetName,
		HTTP: tailscale.OAuthConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{"all:write"},
		}.HTTPClient(),
	}

	return tsClient, nil
}

func (r *PodReconciler) reconcileDeviceTags(ctx context.Context, pod *corev1.Pod, deviceID string) error {
	logger := log.FromContext(ctx)

	tailnetName := pod.Annotations[AnnotationTailnet]
	if tailnetName == "" {
		return nil
	}

	tailnet := &tailcarv1alpha1.Tailnet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      tailnetName,
		Namespace: pod.Namespace,
	}, tailnet); err != nil {
		return fmt.Errorf("failed to get Tailnet: %w", err)
	}

	desiredTags := tailnet.Spec.Tailscale.Tags

	tsClient, err := r.getTailscaleClient(ctx, tailnet)
	if err != nil {
		return fmt.Errorf("failed to create Tailscale client: %w", err)
	}

	device, err := tsClient.Devices().Get(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("failed to get device: %w", err)
	}

	if tagsEqual(device.Tags, desiredTags) {
		return nil
	}

	if err := tsClient.Devices().SetTags(ctx, deviceID, desiredTags); err != nil {
		return fmt.Errorf("failed to update device tags: %w", err)
	}

	logger.Info("Updated device tags", "deviceID", deviceID, "tags", desiredTags)
	return nil
}

func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]bool, len(a))
	for _, tag := range a {
		m[tag] = true
	}
	for _, tag := range b {
		if !m[tag] {
			return false
		}
	}
	return true
}

func buildExpectedHostname(pod *corev1.Pod) string {
	return pod.Name
}

func (r *PodReconciler) decrementInjectedPods(ctx context.Context, namespace, tailnetName string) error {
	tailnet := &tailcarv1alpha1.Tailnet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      tailnetName,
		Namespace: namespace,
	}, tailnet); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get Tailnet: %w", err)
	}

	if tailnet.Status.InjectedPods > 0 {
		tailnet.Status.InjectedPods--
	}
	return r.Status().Update(ctx, tailnet)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Watches(&tailcarv1alpha1.Tailnet{}, &tailnetEventHandler{Client: r.Client}).
		Complete(r)
}

type tailnetEventHandler struct {
	client.Client
}

func (h *tailnetEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.RateLimitingInterface) {
	h.enqueuePods(ctx, e.Object, q)
}

func (h *tailnetEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {
	h.enqueuePods(ctx, e.ObjectNew, q)
}

func (h *tailnetEventHandler) Delete(_ context.Context, _ event.DeleteEvent, _ workqueue.RateLimitingInterface) {
}

func (h *tailnetEventHandler) Generic(_ context.Context, _ event.GenericEvent, _ workqueue.RateLimitingInterface) {
}

func (h *tailnetEventHandler) enqueuePods(ctx context.Context, obj client.Object, q workqueue.RateLimitingInterface) {
	tailnet, ok := obj.(*tailcarv1alpha1.Tailnet)
	if !ok {
		return
	}

	pods := &corev1.PodList{}
	if err := h.List(ctx, pods); err != nil {
		return
	}

	for _, pod := range pods.Items {
		if pod.Annotations[AnnotationTailnet] == tailnet.Name && pod.Annotations[AnnotationInjected] == "true" {
			q.Add(ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				},
			})
		}
	}
}
