package main

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	testingfake "k8s.io/client-go/testing"
)

func leaseObject(holder, renew string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "coordination.k8s.io/v1", "kind": "Lease",
		"metadata": map[string]any{"name": leaseName, "namespace": "default", "resourceVersion": "1"},
		"spec":     map[string]any{"holderIdentity": holder, "leaseDurationSeconds": int64(30), "renewTime": renew},
	}}
}

func TestLeaseAcquireAndRelease(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), leaseObject("", ""))
	l, err := NewKubernetesLease(client, "pod-uid", "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	acquired, err := client.Resource(leaseGVR).Namespace("default").Get(context.Background(), leaseName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertMicroTime(t, acquired.Object, "spec", "acquireTime")
	assertMicroTime(t, acquired.Object, "spec", "renewTime")
	if err := l.AssertHeld(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := l.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	obj, err := client.Resource(leaseGVR).Namespace("default").Get(context.Background(), leaseName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	holder, _, _ := unstructured.NestedString(obj.Object, "spec", "holderIdentity")
	if holder != "" {
		t.Fatalf("holder=%q", holder)
	}
}

func TestLeaseUsesConfiguredNamespace(t *testing.T) {
	obj := leaseObject("", "")
	obj.SetNamespace("resolver-check")
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	l, err := NewKubernetesLease(client, "pod", "resolver-check")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resource(leaseGVR).Namespace("resolver-check").Get(context.Background(), leaseName, metav1.GetOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseRejectsActiveOtherHolder(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), leaseObject("other", time.Now().UTC().Format(time.RFC3339Nano)))
	l, _ := NewKubernetesLease(client, "pod", "default")
	if err := l.Acquire(context.Background()); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("err=%v", err)
	}
}

func TestLeaseExpiredHolderCanBeReplaced(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), leaseObject("other", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)))
	l, _ := NewKubernetesLease(client, "pod", "default")
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := l.AssertHeld(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseRenewUpdatesOnlyOwnLease(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), leaseObject("pod", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)))
	l, _ := NewKubernetesLease(client, "pod", "default")
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := client.Resource(leaseGVR).Namespace("default").Get(context.Background(), leaseName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.renewOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := client.Resource(leaseGVR).Namespace("default").Get(context.Background(), leaseName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	old, _, _ := unstructured.NestedString(before.Object, "spec", "renewTime")
	fresh, _, _ := unstructured.NestedString(after.Object, "spec", "renewTime")
	if old == fresh {
		t.Fatal("renewTime was not advanced")
	}
	assertMicroTime(t, after.Object, "spec", "renewTime")
}

func TestLeaseConflictRetriesAndVerifiesHolderBeforeRelease(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), leaseObject("", ""))
	resource := schema.GroupResource{Group: leaseGVR.Group, Resource: leaseGVR.Resource}
	updates := 0
	client.PrependReactor("update", "leases", func(a testingfake.Action) (bool, runtime.Object, error) {
		updates++
		if updates == 1 {
			return true, nil, apierrors.NewConflict(resource, leaseName, errors.New("conflict"))
		}
		return false, nil, nil
	})
	l, _ := NewKubernetesLease(client, "pod", "default")
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if updates != 2 {
		t.Fatalf("updates=%d", updates)
	}
	client.PrependReactor("get", "leases", func(a testingfake.Action) (bool, runtime.Object, error) {
		obj := leaseObject("other", time.Now().UTC().Format(time.RFC3339Nano))
		return true, obj, nil
	})
	if err := l.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseRenewalFailureMarksOwnershipLost(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), leaseObject("other", time.Now().UTC().Format(time.RFC3339Nano)))
	l, _ := NewKubernetesLease(client, "pod", "default")
	l.stateMu.Lock()
	l.held = true
	l.stateMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	l.Renew(ctx)
	if err := l.AssertHeld(context.Background()); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("err=%v", err)
	}
}

func assertMicroTime(t *testing.T, object map[string]any, fields ...string) {
	t.Helper()
	value, found, err := unstructured.NestedString(object, fields...)
	if err != nil || !found {
		t.Fatalf("%v is missing: found=%t err=%v", fields, found, err)
	}
	parsed, err := time.Parse(metav1.RFC3339Micro, value)
	if err != nil {
		t.Fatalf("%v=%q is not a Kubernetes MicroTime: %v", fields, value, err)
	}
	if got := parsed.UTC().Format(metav1.RFC3339Micro); got != value {
		t.Fatalf("%v=%q is not canonical MicroTime %q", fields, value, got)
	}
}
