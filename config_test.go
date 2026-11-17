package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestLoadConfigRequiresEveryInput(t *testing.T) {
	keys := []string{"OCI_MANIFEST_REGION", "OCI_MANIFEST_COMPARTMENT_ID", "OCI_MANIFEST_VAULT_NAME", "OCI_MANIFEST_SECRET_NAME", "POD_UID"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected missing input error")
	}
	for _, key := range keys {
		t.Setenv(key, "value")
	}
	t.Setenv("OCI_MANIFEST_COMPARTMENT_ID", "ocid1.compartment.oc1..example")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "value" || cfg.PodUID != "value" || cfg.Namespace != "default" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func setValidConfig(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{"OCI_MANIFEST_REGION": "ap-tokyo-1", "OCI_MANIFEST_COMPARTMENT_ID": "ocid1.compartment.oc1..example", "OCI_MANIFEST_VAULT_NAME": "vault", "OCI_MANIFEST_SECRET_NAME": "manifest", "POD_UID": "pod", "POD_NAMESPACE": "default"} {
		t.Setenv(key, value)
	}
}

func TestLoadConfigRejectsUnsafeFields(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"OCI_MANIFEST_REGION", "bad region"},
		{"OCI_MANIFEST_COMPARTMENT_ID", "ocid1.vault.oc1..example"},
		{"POD_NAMESPACE", "bad_namespace"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			setValidConfig(t)
			t.Setenv(tc.key, tc.value)
			if _, err := loadConfig(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected %s=%q to be rejected, got %v", tc.key, tc.value, err)
			}
		})
	}
}

// Keep in sync with the format rules in cluster/bootstrap/bundle.cue.
func TestLoadConfigAcceptsBundleValues(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"OCI_MANIFEST_VAULT_NAME", strings.Repeat("あ", 100)},
		{"OCI_MANIFEST_VAULT_NAME", " vault "},
		{"OCI_MANIFEST_VAULT_NAME", " "},
		{"OCI_MANIFEST_SECRET_NAME", strings.Repeat("s", 300)},
	} {
		t.Run(tc.key, func(t *testing.T) {
			setValidConfig(t)
			t.Setenv(tc.key, tc.value)
			cfg, err := loadConfig()
			if err != nil {
				t.Fatalf("expected %s=%q to be accepted, got %v", tc.key, tc.value, err)
			}
			if got := map[string]string{"OCI_MANIFEST_VAULT_NAME": cfg.VaultName, "OCI_MANIFEST_SECRET_NAME": cfg.SecretName}[tc.key]; got != tc.value {
				t.Fatalf("expected %s to be used as given, got %q", tc.key, got)
			}
		})
	}
}

func TestLoadConfigAcceptsNamespace(t *testing.T) {
	for key, value := range map[string]string{"OCI_MANIFEST_REGION": "ap-tokyo-1", "OCI_MANIFEST_COMPARTMENT_ID": "ocid1.compartment.oc1..example", "OCI_MANIFEST_VAULT_NAME": "vault", "OCI_MANIFEST_SECRET_NAME": "manifest", "POD_UID": "pod", "POD_NAMESPACE": "resolver-check"} {
		t.Setenv(key, value)
	}
	cfg, err := loadConfig()
	if err != nil || cfg.Namespace != "resolver-check" {
		t.Fatalf("unexpected config: %#v, %v", cfg, err)
	}
}

func TestLoadConfigAcceptsTenancyAsRootCompartment(t *testing.T) {
	for key, value := range map[string]string{"OCI_MANIFEST_REGION": "ap-tokyo-1", "OCI_MANIFEST_COMPARTMENT_ID": "ocid1.tenancy.oc1..example", "OCI_MANIFEST_VAULT_NAME": "vault", "OCI_MANIFEST_SECRET_NAME": "manifest", "POD_UID": "pod"} {
		t.Setenv(key, value)
	}
	cfg, err := loadConfig()
	if err != nil || cfg.CompartmentID != "ocid1.tenancy.oc1..example" {
		t.Fatalf("unexpected config: %#v, %v", cfg, err)
	}
}

func TestVersionNeedsNoConfiguration(t *testing.T) {
	old := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	if err := run(context.Background(), []string{"--version"}); err != nil {
		t.Fatal(err)
	}
	_ = write.Close()
	os.Stdout = old
	out, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("version output was empty")
	}
	if err := run(context.Background(), []string{"--version", "extra"}); err == nil {
		t.Fatal("expected invalid arguments")
	}
}
