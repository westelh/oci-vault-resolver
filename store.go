package main

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	storeName      = "platform-manifest"
	storeNamespace = "default"
	managedByKey   = "my-cloud.westelh.dev/managed-by"
	managedByValue = "platform-vault-resolver"
)

var secretStoreGVR = schema.GroupVersionResource{Group: "external-secrets.io", Version: "v1", Resource: "secretstores"}
var ErrStoreOwnership = errors.New("SecretStore is not owned by the resolver")

type KubernetesStore struct{ Client dynamic.Interface }

func NewKubernetesStore(client dynamic.Interface) *KubernetesStore {
	return &KubernetesStore{Client: client}
}

func (s *KubernetesStore) Ensure(ctx context.Context, cfg Config, vaultID string, assertHeld func(context.Context) error) error {
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = storeNamespace
	}
	resource := s.Client.Resource(secretStoreGVR).Namespace(namespace)
	for attempt := 0; attempt < 5; attempt++ {
		if assertHeld != nil {
			if err := assertHeld(ctx); err != nil {
				return err
			}
		}
		current, err := resource.Get(ctx, storeName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			obj := desiredSecretStore(namespace, cfg.Region, vaultID, cfg.CompartmentID)
			if assertHeld != nil {
				if err := assertHeld(ctx); err != nil {
					return err
				}
			}
			_, createErr := resource.Create(ctx, obj, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(createErr) {
				continue
			}
			if apierrors.IsConflict(createErr) {
				continue
			}
			if createErr != nil {
				return fmt.Errorf("create SecretStore: %w", createErr)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("get SecretStore: %w", err)
		}
		if err := validateStoreOwnership(current.Object); err != nil {
			return err
		}
		currentOracle, found, err := unstructured.NestedMap(current.Object, "spec", "provider", "oracle")
		if err != nil || !found || !sameRequiredSetting(currentOracle, "region", cfg.Region) ||
			!sameRequiredSetting(currentOracle, "compartment", cfg.CompartmentID) ||
			!sameRequiredSetting(currentOracle, "vault", vaultID) ||
			!sameOptionalSetting(currentOracle, "principalType", "InstancePrincipal") {
			return ErrStoreOwnership
		}
		if isDesiredStore(current.Object, cfg.Region, vaultID, cfg.CompartmentID) {
			return nil
		}
		updated := current.DeepCopy()
		if err := unstructured.SetNestedField(updated.Object, map[string]any{"oracle": map[string]any{
			"region": cfg.Region, "vault": vaultID, "compartment": cfg.CompartmentID,
		}}, "spec", "provider"); err != nil {
			return err
		}
		updated.SetAnnotations(mergeMap(updated.GetAnnotations(), map[string]string{managedByKey: managedByValue}))
		if assertHeld != nil {
			if err := assertHeld(ctx); err != nil {
				return err
			}
		}
		_, err = resource.Update(ctx, updated, metav1.UpdateOptions{FieldManager: managedByValue})
		if apierrors.IsConflict(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("update SecretStore: %w", err)
		}
		return nil
	}
	return fmt.Errorf("update SecretStore: conflict retry limit reached")
}

// A managed-by annotation permits resolver updates only when the complete
// existing provider identity is already known and matches the resolved target.
// Missing required values are ambiguous and must be repaired by an operator.
func sameRequiredSetting(settings map[string]any, key, expected string) bool {
	value, ok := settings[key].(string)
	return ok && value != "" && value == expected
}

func sameOptionalSetting(settings map[string]any, key, expected string) bool {
	value, exists := settings[key]
	if !exists {
		return true
	}
	actual, ok := value.(string)
	return ok && actual == expected
}

func desiredSecretStore(namespace, region, vaultID, compartmentID string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "external-secrets.io/v1", "kind": "SecretStore",
		"metadata": map[string]any{"name": storeName, "namespace": namespace, "annotations": map[string]any{managedByKey: managedByValue}},
		"spec": map[string]any{"provider": map[string]any{"oracle": map[string]any{
			"region": region, "vault": vaultID, "compartment": compartmentID,
		}}},
	}}
}

func validateStoreOwnership(obj map[string]any) error {
	metadata, _ := obj["metadata"].(map[string]any)
	annotations, _ := metadata["annotations"].(map[string]any)
	if annotations[managedByKey] != managedByValue {
		return ErrStoreOwnership
	}
	if _, exists := metadata["ownerReferences"]; exists {
		return ErrStoreOwnership
	}
	spec, _ := obj["spec"].(map[string]any)
	provider, _ := spec["provider"].(map[string]any)
	if len(provider) != 1 || provider["oracle"] == nil {
		return ErrStoreOwnership
	}
	oracle, _ := provider["oracle"].(map[string]any)
	if oracle["auth"] != nil || oracle["serviceAccountRef"] != nil {
		return ErrStoreOwnership
	}
	return nil
}

func isDesiredStore(obj map[string]any, region, vaultID, compartmentID string) bool {
	spec, _ := obj["spec"].(map[string]any)
	provider, _ := spec["provider"].(map[string]any)
	oracle, _ := provider["oracle"].(map[string]any)
	metadata, _ := obj["metadata"].(map[string]any)
	annotations, _ := metadata["annotations"].(map[string]any)
	principal, principalPresent := oracle["principalType"]
	return oracle["region"] == region && oracle["vault"] == vaultID && oracle["compartment"] == compartmentID && (!principalPresent || principal == "InstancePrincipal") &&
		oracle["auth"] == nil && oracle["serviceAccountRef"] == nil &&
		annotations[managedByKey] == managedByValue && len(provider) == 1
}

func mergeMap(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
