package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	tailcarv1alpha1 "github.com/rajsinghtech/tailcar/api/v1alpha1"
	"tailscale.com/client/tailscale/v2"
)

const (
	tailnetFinalizer = "tailcar.rajsingh.info/finalizer"

	// Condition types
	ConditionTypeReady          = "Ready"
	ConditionTypeOAuthValid     = "OAuthValid"
	ConditionTypeAuthKeyCreated = "AuthKeyCreated"

	// Condition reasons
	ReasonOAuthValidationFailed = "OAuthValidationFailed"
	ReasonOAuthValid            = "OAuthValid"
	ReasonAuthKeyCreationFailed = "AuthKeyCreationFailed"
	ReasonAuthKeyCreated        = "AuthKeyCreated"
	ReasonReconcileSuccess      = "ReconcileSuccess"
	ReasonReconcileError        = "ReconcileError"
)

type TailnetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=tailcar.rajsingh.info,resources=tailnets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tailcar.rajsingh.info,resources=tailnets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tailcar.rajsingh.info,resources=tailnets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *TailnetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	tailnet := &tailcarv1alpha1.Tailnet{}
	if err := r.Get(ctx, req.NamespacedName, tailnet); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Tailnet")
		return ctrl.Result{}, err
	}

	if !tailnet.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, tailnet)
	}

	if !controllerutil.ContainsFinalizer(tailnet, tailnetFinalizer) {
		controllerutil.AddFinalizer(tailnet, tailnetFinalizer)
		if err := r.Update(ctx, tailnet); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Added finalizer")
		// No need to requeue - the update will trigger a new reconciliation
		return ctrl.Result{}, nil
	}

	tsClient, err := r.getTailscaleClient(ctx, tailnet)
	if err != nil {
		logger.Error(err, "Failed to get Tailscale client")
		return r.updateStatus(ctx, tailnet, metav1.ConditionFalse, ConditionTypeOAuthValid,
			ReasonOAuthValidationFailed, err.Error())
	}

	meta.SetStatusCondition(&tailnet.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeOAuthValid,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: tailnet.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonOAuthValid,
		Message:            "OAuth credentials are valid",
	})

	if err := r.ensureAuthKey(ctx, tailnet, tsClient); err != nil {
		logger.Error(err, "Failed to ensure auth key")
		return r.updateStatus(ctx, tailnet, metav1.ConditionFalse, ConditionTypeAuthKeyCreated,
			ReasonAuthKeyCreationFailed, err.Error())
	}

	return r.updateStatus(ctx, tailnet, metav1.ConditionTrue, ConditionTypeReady,
		ReasonReconcileSuccess, "Tailnet is ready")
}

func (r *TailnetReconciler) handleDeletion(ctx context.Context, tailnet *tailcarv1alpha1.Tailnet) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(tailnet, tailnetFinalizer) {
		tsClient, err := r.getTailscaleClient(ctx, tailnet)
		if err != nil {
			logger.Error(err, "Failed to get Tailscale client for cleanup")
		} else if tailnet.Status.AuthKeyID != "" {
			if err := tsClient.Keys().Delete(ctx, tailnet.Status.AuthKeyID); err != nil {
				logger.Error(err, "Failed to delete auth key", "keyID", tailnet.Status.AuthKeyID)
			} else {
				logger.Info("Deleted auth key", "keyID", tailnet.Status.AuthKeyID)
			}
		}

		secretName := fmt.Sprintf("%s-authkey", tailnet.Name)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: tailnet.Namespace,
			},
		}
		if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "Failed to delete auth key secret")
			return ctrl.Result{}, err
		}

		controllerutil.RemoveFinalizer(tailnet, tailnetFinalizer)
		if err := r.Update(ctx, tailnet); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Removed finalizer")
	}

	return ctrl.Result{}, nil
}

func (r *TailnetReconciler) getTailscaleClient(ctx context.Context, tailnet *tailcarv1alpha1.Tailnet) (*tailscale.Client, error) {
	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{
		Name:      tailnet.Spec.OAuthSecretRef.Name,
		Namespace: tailnet.Spec.OAuthSecretRef.Namespace,
	}

	if err := r.Get(ctx, secretKey, secret); err != nil {
		return nil, fmt.Errorf("failed to get OAuth secret: %w", err)
	}

	clientID, ok := secret.Data["client-id"]
	if !ok {
		return nil, fmt.Errorf("client-id not found in secret")
	}

	clientSecret, ok := secret.Data["client-secret"]
	if !ok {
		return nil, fmt.Errorf("client-secret not found in secret")
	}

	tsClient := &tailscale.Client{
		Tailnet: tailnet.Spec.TailnetName,
		HTTP: tailscale.OAuthConfig{
			ClientID:     string(clientID),
			ClientSecret: string(clientSecret),
			Scopes:       []string{"all:write"},
		}.HTTPClient(),
	}

	return tsClient, nil
}

func (r *TailnetReconciler) ensureAuthKey(ctx context.Context, tailnet *tailcarv1alpha1.Tailnet, tsClient *tailscale.Client) error {
	logger := log.FromContext(ctx)

	if tailnet.Status.AuthKeyID != "" {
		key, err := tsClient.Keys().Get(ctx, tailnet.Status.AuthKeyID)
		if err == nil && !key.Invalid && key.Revoked.IsZero() && key.Expires.After(time.Now()) {
			secretName := fmt.Sprintf("%s-authkey", tailnet.Name)
			secret := &corev1.Secret{}
			secretKey := client.ObjectKey{Name: secretName, Namespace: tailnet.Namespace}
			if err := r.Get(ctx, secretKey, secret); err != nil {
				logger.Info("Auth key secret missing, recreating secret only", "keyID", tailnet.Status.AuthKeyID)
				// Secret is missing but key is valid - just recreate the secret
				// We can't retrieve the key value from Tailscale, so we have to create a new key
				if err := tsClient.Keys().Delete(ctx, tailnet.Status.AuthKeyID); err != nil {
					logger.Error(err, "Failed to delete old auth key")
				}
				// Fall through to create new key
			} else {
				// Both key and secret exist and are valid
				return nil
			}
		}
	}

	tags := tailnet.Spec.Tailscale.Tags
	if len(tags) == 0 {
		tags = []string{"tag:k8s"}
	}

	createReq := tailscale.CreateKeyRequest{
		Capabilities:  tailscale.KeyCapabilities{},
		ExpirySeconds: 90 * 24 * 60 * 60, // 90 days
		Description:   fmt.Sprintf("Tailcar operator %s-%s", tailnet.Namespace, tailnet.Name),
	}

	createReq.Capabilities.Devices.Create.Reusable = true
	createReq.Capabilities.Devices.Create.Ephemeral = true
	createReq.Capabilities.Devices.Create.Preauthorized = tailnet.Spec.Tailscale.AutoApprove
	createReq.Capabilities.Devices.Create.Tags = tags

	key, err := tsClient.Keys().CreateAuthKey(ctx, createReq)
	if err != nil {
		return fmt.Errorf("failed to create auth key: %w", err)
	}

	logger.Info("Created auth key", "keyID", key.ID)

	tailnet.Status.AuthKeyID = key.ID
	tailnet.Status.AuthKeyCreated = &metav1.Time{Time: key.Created}

	meta.SetStatusCondition(&tailnet.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeAuthKeyCreated,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: tailnet.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             ReasonAuthKeyCreated,
		Message:            fmt.Sprintf("Auth key created: %s", key.ID),
	})

	return r.ensureAuthKeySecret(ctx, tailnet, key.Key)
}

func (r *TailnetReconciler) ensureAuthKeySecret(ctx context.Context, tailnet *tailcarv1alpha1.Tailnet, authKey string) error {
	logger := log.FromContext(ctx)

	secretName := fmt.Sprintf("%s-authkey", tailnet.Name)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: tailnet.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		secret.Type = corev1.SecretTypeOpaque
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		secret.Data["TS_AUTHKEY"] = []byte(authKey)

		if err := controllerutil.SetControllerReference(tailnet, secret, r.Scheme); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create/update auth key secret: %w", err)
	}

	logger.Info("Ensured auth key secret", "secretName", secretName)
	return nil
}

func (r *TailnetReconciler) updateStatus(ctx context.Context, tailnet *tailcarv1alpha1.Tailnet,
	status metav1.ConditionStatus, conditionType, reason, message string) (ctrl.Result, error) {

	tailnet.Status.ObservedGeneration = tailnet.Generation

	meta.SetStatusCondition(&tailnet.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: tailnet.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	})

	if err := r.Status().Update(ctx, tailnet); err != nil {
		return ctrl.Result{}, err
	}

	if status == metav1.ConditionFalse {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

func (r *TailnetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tailcarv1alpha1.Tailnet{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
