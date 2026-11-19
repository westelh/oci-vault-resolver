package main

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

var secretsResource = schema.GroupResource{Resource: "secrets"}

func testConfig() Config {
	return Config{Namespace: "resolver-check", SecretKey: "manifest", ManagedBy: "resolver-a"}
}

// managedSecret is a Secret that resolver-a may replace.
func managedSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: targetSecretName, Namespace: "resolver-check", ResourceVersion: "7",
			Annotations: map[string]string{managedByKey: "resolver-a", "other": "annotation"},
			Labels:      map[string]string{"keep": "label"},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"manifest": []byte(`{"old":true}`), "stale": []byte("x")},
	}
}

// recordWrites counts create, update, patch and delete calls on Secrets.
func recordWrites(client *fake.Clientset) *[]string {
	var verbs []string
	client.PrependReactor("*", "secrets", func(a clienttesting.Action) (bool, runtime.Object, error) {
		switch a.GetVerb() {
		case "create", "update", "patch", "delete":
			verbs = append(verbs, a.GetVerb())
		}
		return false, nil, nil
	})
	return &verbs
}

func getTarget(t *testing.T, client *fake.Clientset) *corev1.Secret {
	t.Helper()
	secret, err := client.CoreV1().Secrets("resolver-check").Get(context.Background(), targetSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestWriteCreatesMarkedSecretWhenMissing(t *testing.T) {
	client := fake.NewClientset()
	writes := recordWrites(client)
	content := []byte(`{"region":"ap-tokyo-1"}`)
	if err := (SecretWriter{Client: client}).Write(context.Background(), testConfig(), content); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0] != "create" {
		t.Fatalf("writes=%v", *writes)
	}
	created := getTarget(t, client)
	if created.Annotations[managedByKey] != "resolver-a" || created.Type != corev1.SecretTypeOpaque {
		t.Fatalf("created=%+v", created)
	}
	if len(created.Data) != 1 || string(created.Data["manifest"]) != string(content) {
		t.Fatalf("data=%v", created.Data)
	}
}

func TestWriteReplacesManagedSecretWhole(t *testing.T) {
	client := fake.NewClientset(managedSecret())
	var sentVersion string
	client.PrependReactor("update", "secrets", func(a clienttesting.Action) (bool, runtime.Object, error) {
		sentVersion = a.(clienttesting.UpdateAction).GetObject().(*corev1.Secret).ResourceVersion
		return false, nil, nil
	})
	writes := recordWrites(client)
	content := []byte(`{"new":true}`)
	if err := (SecretWriter{Client: client}).Write(context.Background(), testConfig(), content); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0] != "update" {
		t.Fatalf("writes=%v", *writes)
	}
	if sentVersion != "7" {
		t.Fatalf("update must carry the read resourceVersion, got %q", sentVersion)
	}
	after := getTarget(t, client)
	if len(after.Data) != 1 || string(after.Data["manifest"]) != string(content) {
		t.Fatalf("data=%v", after.Data)
	}
	if len(after.Annotations) != 1 || after.Annotations[managedByKey] != "resolver-a" || len(after.Labels) != 0 {
		t.Fatalf("metadata was not replaced: annotations=%v labels=%v", after.Annotations, after.Labels)
	}
}

func TestWriteRejectsUnmanagedSecretWithoutWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Secret)
	}{
		{"missing marker", func(s *corev1.Secret) { delete(s.Annotations, managedByKey) }},
		{"no annotations", func(s *corev1.Secret) { s.Annotations = nil }},
		{"different marker value", func(s *corev1.Secret) { s.Annotations[managedByKey] = "resolver-b" }},
		{"empty marker value", func(s *corev1.Secret) { s.Annotations[managedByKey] = "" }},
		{"old marker key", func(s *corev1.Secret) {
			delete(s.Annotations, managedByKey)
			s.Annotations["my-cloud.westelh.dev/managed-by"] = "resolver-a"
		}},
		{"owner reference", func(s *corev1.Secret) {
			s.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "owner", UID: "uid"}}
		}},
		{"non-opaque type", func(s *corev1.Secret) { s.Type = corev1.SecretTypeDockerConfigJson }},
		{"immutable", func(s *corev1.Secret) { immutable := true; s.Immutable = &immutable }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := managedSecret()
			tc.mutate(existing)
			client := fake.NewClientset(existing.DeepCopy())
			writes := recordWrites(client)
			err := (SecretWriter{Client: client}).Write(context.Background(), testConfig(), []byte(`{}`))
			if !errors.Is(err, ErrSecretOwnership) || len(*writes) != 0 {
				t.Fatalf("err=%v writes=%v", err, *writes)
			}
			if after := getTarget(t, client); string(after.Data["manifest"]) != `{"old":true}` {
				t.Fatalf("Secret changed: %v", after.Data)
			}
		})
	}
}

func TestWriteFailsWithoutRetryOnCreateRace(t *testing.T) {
	client := fake.NewClientset()
	creates := 0
	client.PrependReactor("create", "secrets", func(a clienttesting.Action) (bool, runtime.Object, error) {
		creates++
		return true, nil, apierrors.NewAlreadyExists(secretsResource, targetSecretName)
	})
	err := (SecretWriter{Client: client}).Write(context.Background(), testConfig(), []byte(`{}`))
	if !errors.Is(err, ErrSecretConflict) || creates != 1 {
		t.Fatalf("err=%v creates=%d", err, creates)
	}
	if got := classify(err); got != "secret_write_conflict" {
		t.Fatalf("classify()=%q", got)
	}
}

func TestWriteDistinguishesUpdateErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		conflict bool
		want     string
	}{
		{"409", apierrors.NewConflict(secretsResource, targetSecretName, errors.New("stale resourceVersion")), true, "secret_write_conflict"},
		{"403", apierrors.NewForbidden(secretsResource, targetSecretName, errors.New("denied")), false, "kubernetes_forbidden"},
		{"404", apierrors.NewNotFound(secretsResource, targetSecretName), false, "kubernetes_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientset(managedSecret())
			updates := 0
			client.PrependReactor("update", "secrets", func(a clienttesting.Action) (bool, runtime.Object, error) {
				updates++
				return true, nil, tc.err
			})
			err := (SecretWriter{Client: client}).Write(context.Background(), testConfig(), []byte(`{}`))
			if err == nil || updates != 1 {
				t.Fatalf("err=%v updates=%d", err, updates)
			}
			if errors.Is(err, ErrSecretConflict) != tc.conflict {
				t.Fatalf("errors.Is(ErrSecretConflict)=%v want %v", !tc.conflict, tc.conflict)
			}
			if got := classify(err); got != tc.want {
				t.Fatalf("classify()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestWriteDistinguishesGetErrors(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("get", "secrets", func(a clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(secretsResource, targetSecretName, errors.New("denied"))
	})
	writes := recordWrites(client)
	err := (SecretWriter{Client: client}).Write(context.Background(), testConfig(), []byte(`{}`))
	if classify(err) != "kubernetes_forbidden" || len(*writes) != 0 {
		t.Fatalf("err=%v class=%q writes=%v", err, classify(err), *writes)
	}
}
