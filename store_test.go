package main

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func testStore() *unstructured.Unstructured {
	obj := desiredSecretStore("default", "ap-tokyo-1", "vault-1", "compartment-1")
	obj.SetResourceVersion("1")
	obj.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}
	obj.SetLabels(map[string]string{"keep": "label"})
	return obj
}

func TestStoreUsesConfiguredNamespace(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	err := NewKubernetesStore(client).Ensure(context.Background(), Config{Namespace: "resolver-check", Region: "ap-tokyo-1", CompartmentID: "compartment-1"}, "vault-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.Resource(secretStoreGVR).Namespace("resolver-check").Get(context.Background(), storeName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetNamespace() != "resolver-check" {
		t.Fatalf("namespace=%q", created.GetNamespace())
	}
}

func TestStoreCreateFixedNameAndAPIPath(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	var actions []clienttesting.Action
	client.PrependReactor("*", "*", func(a clienttesting.Action) (bool, runtime.Object, error) {
		actions = append(actions, a)
		return false, nil, nil
	})
	err := NewKubernetesStore(client).Ensure(context.Background(), Config{Region: "ap-tokyo-1", CompartmentID: "compartment-1"}, "vault-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[0].GetVerb() != "get" || actions[1].GetVerb() != "create" {
		t.Fatalf("actions=%v", actions)
	}
	for _, a := range actions {
		if a.GetNamespace() != storeNamespace {
			t.Fatalf("namespace=%q", a.GetNamespace())
		}
	}
	created, err := client.Resource(secretStoreGVR).Namespace("default").Get(context.Background(), storeName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetName() != storeName || created.GetAnnotations()[managedByKey] != managedByValue {
		t.Fatalf("created=%v", created.Object)
	}
	for _, action := range client.Actions() {
		if action.GetResource().Group == "external-secrets.io" && action.GetResource().Resource != "secretstores" {
			t.Fatalf("unexpected API resource: %v", action.GetResource())
		}
	}
}

func TestStoreNoopPreservesOtherMetadataAndStatus(t *testing.T) {
	obj := testStore()
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	updates := 0
	client.PrependReactor("update", "secretstores", func(a clienttesting.Action) (bool, runtime.Object, error) { updates++; return false, nil, nil })
	err := NewKubernetesStore(client).Ensure(context.Background(), Config{Region: "ap-tokyo-1", CompartmentID: "compartment-1"}, "vault-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if updates != 0 {
		t.Fatalf("updates=%d", updates)
	}
	after, err := client.Resource(secretStoreGVR).Namespace("default").Get(context.Background(), storeName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if after.GetLabels()["keep"] != "label" || after.Object["status"] == nil {
		t.Fatalf("lost preserved fields: %v", after.Object)
	}
}

func TestStoreRejectsUnownedOrAmbiguousProviderWithoutWrite(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*unstructured.Unstructured)
	}{
		{name: "missing marker", mutate: func(o *unstructured.Unstructured) { o.SetAnnotations(nil) }},
		{name: "different provider", mutate: func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, "key", "spec", "provider", "aws")
		}},
		{name: "owner reference", mutate: func(o *unstructured.Unstructured) { o.SetOwnerReferences([]metav1.OwnerReference{{Name: "owner"}}) }},
		{name: "auth configured", mutate: func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, map[string]any{}, "spec", "provider", "oracle", "auth")
		}},
		{name: "missing vault", mutate: func(o *unstructured.Unstructured) {
			unstructured.RemoveNestedField(o.Object, "spec", "provider", "oracle", "vault")
		}},
		{name: "different vault", mutate: func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, "other-vault", "spec", "provider", "oracle", "vault")
		}},
		{name: "missing compartment", mutate: func(o *unstructured.Unstructured) {
			unstructured.RemoveNestedField(o.Object, "spec", "provider", "oracle", "compartment")
		}},
		{name: "different compartment", mutate: func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, "other-compartment", "spec", "provider", "oracle", "compartment")
		}},
		{name: "missing region", mutate: func(o *unstructured.Unstructured) {
			unstructured.RemoveNestedField(o.Object, "spec", "provider", "oracle", "region")
		}},
		{name: "invalid principal", mutate: func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, "UserPrincipal", "spec", "provider", "oracle", "principalType")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			obj := testStore()
			test.mutate(obj)
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
			writes := 0
			client.PrependReactor("update", "*", func(a clienttesting.Action) (bool, runtime.Object, error) { writes++; return false, nil, nil })
			err := NewKubernetesStore(client).Ensure(context.Background(), Config{Region: "ap-tokyo-1", CompartmentID: "compartment-1"}, "vault-1", nil)
			if !errors.Is(err, ErrStoreOwnership) || writes != 0 {
				t.Fatalf("err=%v writes=%d", err, writes)
			}
		})
	}
}

func TestStoreCreateConflictRetries(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	creates := 0
	groupResource := schema.GroupResource{Group: secretStoreGVR.Group, Resource: secretStoreGVR.Resource}
	client.PrependReactor("create", "secretstores", func(a clienttesting.Action) (bool, runtime.Object, error) {
		creates++
		if creates == 1 {
			return true, nil, apierrors.NewConflict(groupResource, storeName, errors.New("create race"))
		}
		return false, nil, nil
	})
	err := NewKubernetesStore(client).Ensure(context.Background(), Config{Region: "ap-tokyo-1", CompartmentID: "compartment-1"}, "vault-1", nil)
	if err != nil || creates != 2 {
		t.Fatalf("err=%v creates=%d", err, creates)
	}
}

func TestStoreVerifiesLeaseImmediatelyBeforeWrites(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	checks, writes := 0, 0
	client.PrependReactor("*", "*", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetVerb() == "create" {
			writes++
		}
		return false, nil, nil
	})
	assertHeld := func(context.Context) error {
		checks++
		if checks == 2 {
			return ErrLeaseLost
		}
		return nil
	}
	err := NewKubernetesStore(client).Ensure(context.Background(), Config{Region: "ap-tokyo-1", CompartmentID: "compartment-1", SecretName: "manifest"}, "vault-1", assertHeld)
	if !errors.Is(err, ErrLeaseLost) || writes != 0 {
		t.Fatalf("err=%v writes=%d checks=%d", err, writes, checks)
	}
}
