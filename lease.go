package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const leaseName = "platform-vault-resolver"

var leaseGVR = schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}
var (
	ErrLeaseHeld = errors.New("resolver Lease is held by another pod")
	ErrLeaseLost = errors.New("resolver Lease ownership was lost")
)

type KubernetesLease struct {
	Client    dynamic.Interface
	Holder    string
	Namespace string
	held      bool
	stateMu   sync.Mutex
}

func NewKubernetesLease(client dynamic.Interface, podUID, namespace string) (*KubernetesLease, error) {
	if podUID == "" {
		return nil, fmt.Errorf("POD_UID is required")
	}
	if namespace == "" {
		namespace = "default"
	}
	return &KubernetesLease{Client: client, Holder: podUID, Namespace: namespace}, nil
}

func (l *KubernetesLease) Acquire(ctx context.Context) error {
	resource := l.Client.Resource(leaseGVR).Namespace(l.Namespace)
	for attempt := 0; attempt < 5; attempt++ {
		obj, err := resource.Get(ctx, leaseName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get resolver Lease: %w", err)
		}
		now := time.Now()
		current, _, _ := unstructured.NestedString(obj.Object, "spec", "holderIdentity")
		duration, _, _ := unstructured.NestedInt64(obj.Object, "spec", "leaseDurationSeconds")
		if duration <= 0 {
			duration = 30
		}
		renew, _, _ := unstructured.NestedString(obj.Object, "spec", "renewTime")
		if renew == "" {
			renew, _, _ = unstructured.NestedString(obj.Object, "spec", "acquireTime")
		}
		if current != "" && current != l.Holder && renew != "" {
			parsed, parseErr := time.Parse(time.RFC3339Nano, renew)
			if parseErr != nil || time.Since(parsed) < time.Duration(duration)*time.Second {
				return ErrLeaseHeld
			}
		}
		if current != "" && current != l.Holder && renew == "" {
			return ErrLeaseHeld
		}
		if err := unstructured.SetNestedField(obj.Object, l.Holder, "spec", "holderIdentity"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(obj.Object, int64(30), "spec", "leaseDurationSeconds"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(obj.Object, formatMicroTime(now), "spec", "renewTime"); err != nil {
			return err
		}
		if current == "" {
			if err := unstructured.SetNestedField(obj.Object, formatMicroTime(now), "spec", "acquireTime"); err != nil {
				return err
			}
		}
		if _, err = resource.Update(ctx, obj, metav1.UpdateOptions{FieldManager: managedByValue}); apierrors.IsConflict(err) {
			if attempt < 4 {
				if err := leaseBackoff(ctx, attempt); err != nil {
					return err
				}
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("acquire resolver Lease: %w", err)
		}
		l.stateMu.Lock()
		l.held = true
		l.stateMu.Unlock()
		return nil
	}
	return fmt.Errorf("acquire resolver Lease: conflict retry limit reached")
}

func (l *KubernetesLease) AssertHeld(ctx context.Context) error {
	l.stateMu.Lock()
	held := l.held
	l.stateMu.Unlock()
	if !held {
		return ErrLeaseLost
	}
	obj, err := l.Client.Resource(leaseGVR).Namespace(l.Namespace).Get(ctx, leaseName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("verify resolver Lease: %w", err)
	}
	current, _, _ := unstructured.NestedString(obj.Object, "spec", "holderIdentity")
	if current != l.Holder {
		l.stateMu.Lock()
		l.held = false
		l.stateMu.Unlock()
		return ErrLeaseLost
	}
	renew, _, _ := unstructured.NestedString(obj.Object, "spec", "renewTime")
	if renew == "" {
		l.stateMu.Lock()
		l.held = false
		l.stateMu.Unlock()
		return ErrLeaseLost
	}
	parsed, parseErr := time.Parse(time.RFC3339Nano, renew)
	duration, _, _ := unstructured.NestedInt64(obj.Object, "spec", "leaseDurationSeconds")
	if duration <= 0 {
		duration = 30
	}
	if parseErr != nil || time.Since(parsed) >= time.Duration(duration)*time.Second {
		l.stateMu.Lock()
		l.held = false
		l.stateMu.Unlock()
		return ErrLeaseLost
	}
	return nil
}

func (l *KubernetesLease) Renew(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			err := l.renewOnce(renewCtx)
			cancel()
			if err != nil {
				l.stateMu.Lock()
				l.held = false
				l.stateMu.Unlock()
				return
			}
		}
	}
}

func (l *KubernetesLease) renewOnce(ctx context.Context) error {
	resource := l.Client.Resource(leaseGVR).Namespace(l.Namespace)
	for attempt := 0; attempt < 5; attempt++ {
		obj, err := resource.Get(ctx, leaseName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		current, _, _ := unstructured.NestedString(obj.Object, "spec", "holderIdentity")
		if current != l.Holder {
			return ErrLeaseLost
		}
		if err := unstructured.SetNestedField(obj.Object, formatMicroTime(time.Now()), "spec", "renewTime"); err != nil {
			return err
		}
		_, err = resource.Update(ctx, obj, metav1.UpdateOptions{FieldManager: managedByValue})
		if apierrors.IsConflict(err) {
			if err := leaseBackoff(ctx, attempt); err != nil {
				return err
			}
			continue
		}
		return err
	}
	return fmt.Errorf("renew resolver Lease: conflict retry limit reached")
}

func (l *KubernetesLease) Release(ctx context.Context) error {
	l.stateMu.Lock()
	held := l.held
	l.stateMu.Unlock()
	if !held {
		return nil
	}
	resource := l.Client.Resource(leaseGVR).Namespace(l.Namespace)
	for attempt := 0; attempt < 5; attempt++ {
		obj, err := resource.Get(ctx, leaseName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		current, _, _ := unstructured.NestedString(obj.Object, "spec", "holderIdentity")
		if current != l.Holder {
			l.stateMu.Lock()
			l.held = false
			l.stateMu.Unlock()
			return nil
		}
		_ = unstructured.SetNestedField(obj.Object, "", "spec", "holderIdentity")
		if _, err = resource.Update(ctx, obj, metav1.UpdateOptions{FieldManager: managedByValue}); apierrors.IsConflict(err) {
			if attempt < 4 {
				if err := leaseBackoff(ctx, attempt); err != nil {
					return err
				}
			}
			continue
		}
		if err != nil {
			return err
		}
		l.stateMu.Lock()
		l.held = false
		l.stateMu.Unlock()
		return nil
	}
	return fmt.Errorf("release resolver Lease: conflict retry limit reached")
}

func leaseBackoff(ctx context.Context, attempt int) error {
	d := time.Duration(1<<attempt)*20*time.Millisecond + time.Duration(rand.Int63n(int64(20*time.Millisecond)))
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func formatMicroTime(t time.Time) string {
	return t.UTC().Format(metav1.RFC3339Micro)
}
