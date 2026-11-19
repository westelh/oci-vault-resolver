package main

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	targetSecretName = "platform-manifest"
	managedByKey     = "oci-vault-resolver.westelh.dev/managed-by"
	defaultManagedBy = "oci-vault-resolver"
	fieldManager     = "oci-vault-resolver"
)

var (
	ErrSecretOwnership = errors.New("Kubernetes Secret is not managed by this resolver")
	ErrSecretConflict  = errors.New("Kubernetes Secret was changed concurrently")
)

type SecretWriter struct{ Client kubernetes.Interface }

// Write creates the target Secret, or replaces it whole when it carries this
// resolver's managed-by marker and no owner reference. Any other Secret is
// left unchanged. Writes never retry: a create that finds the Secret already
// there, or an update whose resourceVersion is stale, fails with
// ErrSecretConflict.
func (w SecretWriter) Write(ctx context.Context, cfg Config, content []byte) error {
	secrets := w.Client.CoreV1().Secrets(cfg.Namespace)
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: targetSecretName, Namespace: cfg.Namespace,
			Annotations: map[string]string{managedByKey: cfg.ManagedBy},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{cfg.SecretKey: content},
	}
	current, err := secrets.Get(ctx, targetSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(ctx, desired, metav1.CreateOptions{FieldManager: fieldManager})
		if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
			return fmt.Errorf("create Secret: %w: %w", ErrSecretConflict, err)
		}
		if err != nil {
			return fmt.Errorf("create Secret: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get Secret: %w", err)
	}
	if err := checkManaged(current, cfg.ManagedBy); err != nil {
		return err
	}
	desired.ResourceVersion = current.ResourceVersion
	_, err = secrets.Update(ctx, desired, metav1.UpdateOptions{FieldManager: fieldManager})
	if apierrors.IsConflict(err) {
		return fmt.Errorf("update Secret: %w: %w", ErrSecretConflict, err)
	}
	if err != nil {
		return fmt.Errorf("update Secret: %w", err)
	}
	return nil
}

// checkManaged accepts only a Secret that this resolver could have created:
// the marker value matches, nothing owns it, and its immutable type is Opaque.
func checkManaged(secret *corev1.Secret, managedBy string) error {
	if value, ok := secret.Annotations[managedByKey]; !ok || value != managedBy {
		return fmt.Errorf("%w: managed-by annotation is missing or different", ErrSecretOwnership)
	}
	if len(secret.OwnerReferences) != 0 {
		return fmt.Errorf("%w: Secret has an owner reference", ErrSecretOwnership)
	}
	if secret.Type != corev1.SecretTypeOpaque {
		return fmt.Errorf("%w: Secret type is not Opaque", ErrSecretOwnership)
	}
	if secret.Immutable != nil && *secret.Immutable {
		return fmt.Errorf("%w: Secret is immutable", ErrSecretOwnership)
	}
	return nil
}
