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
	keys := []string{"OCI_VAULT_RESOLVER_REGION", "OCI_VAULT_RESOLVER_COMPARTMENT_ID", "OCI_VAULT_RESOLVER_VAULT_NAME", "OCI_VAULT_RESOLVER_SECRET_NAME", "OCI_VAULT_RESOLVER_SECRET_KEY"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	if _, err := loadConfig(); err == nil {
		t.Fatal("expected missing input error")
	}
	t.Setenv("OCI_VAULT_RESOLVER_MANAGED_BY", "")
	t.Setenv("OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME", "")
	for _, key := range keys {
		t.Setenv(key, "value")
	}
	t.Setenv("OCI_VAULT_RESOLVER_COMPARTMENT_ID", "ocid1.compartment.oc1..example")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "value" || cfg.SecretKey != "value" || cfg.Namespace != "default" || cfg.ManagedBy != "oci-vault-resolver" || cfg.KubernetesSecretName != "oci-vault-resolver" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func setValidConfig(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{"OCI_VAULT_RESOLVER_REGION": "ap-tokyo-1", "OCI_VAULT_RESOLVER_COMPARTMENT_ID": "ocid1.compartment.oc1..example", "OCI_VAULT_RESOLVER_VAULT_NAME": "vault", "OCI_VAULT_RESOLVER_SECRET_NAME": "config", "OCI_VAULT_RESOLVER_SECRET_KEY": "config", "OCI_VAULT_RESOLVER_MANAGED_BY": "", "OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME": "", "POD_NAMESPACE": "default"} {
		t.Setenv(key, value)
	}
}

func TestLoadConfigRejectsUnsafeFields(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"OCI_VAULT_RESOLVER_REGION", "bad region"},
		{"OCI_VAULT_RESOLVER_COMPARTMENT_ID", "ocid1.vault.oc1..example"},
		{"POD_NAMESPACE", "bad_namespace"},
		{"OCI_VAULT_RESOLVER_SECRET_KEY", "app.config"},
		{"OCI_VAULT_RESOLVER_SECRET_KEY", "bad/key"},
		{"OCI_VAULT_RESOLVER_SECRET_KEY", "app-config"},
		{"OCI_VAULT_RESOLVER_SECRET_KEY", "_config"},
		{"OCI_VAULT_RESOLVER_SECRET_KEY", "1config"},
		{"OCI_VAULT_RESOLVER_SECRET_KEY", strings.Repeat("k", 254)},
		{"OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME", "App-config"},
		{"OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME", "app_config"},
		{"OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME", "app/config"},
		{"OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME", "-app-config"},
		{"OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME", "app-config."},
		{"OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME", " app-config"},
		{"OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME", strings.Repeat("s", 254)},
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

// The Vault and Secret names are not checked beyond being non-empty, so any
// name OCI accepts is used as given.
func TestLoadConfigUsesOCINamesAsGiven(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"OCI_VAULT_RESOLVER_VAULT_NAME", strings.Repeat("あ", 100)},
		{"OCI_VAULT_RESOLVER_VAULT_NAME", " vault "},
		{"OCI_VAULT_RESOLVER_VAULT_NAME", " "},
		{"OCI_VAULT_RESOLVER_SECRET_NAME", strings.Repeat("s", 300)},
	} {
		t.Run(tc.key, func(t *testing.T) {
			setValidConfig(t)
			t.Setenv(tc.key, tc.value)
			cfg, err := loadConfig()
			if err != nil {
				t.Fatalf("expected %s=%q to be accepted, got %v", tc.key, tc.value, err)
			}
			if got := map[string]string{"OCI_VAULT_RESOLVER_VAULT_NAME": cfg.VaultName, "OCI_VAULT_RESOLVER_SECRET_NAME": cfg.SecretName}[tc.key]; got != tc.value {
				t.Fatalf("expected %s to be used as given, got %q", tc.key, got)
			}
		})
	}
}

func TestLoadConfigAcceptsSecretKeyAndManagedBy(t *testing.T) {
	setValidConfig(t)
	t.Setenv("OCI_VAULT_RESOLVER_SECRET_KEY", "App_config1")
	t.Setenv("OCI_VAULT_RESOLVER_MANAGED_BY", "resolver-a")
	cfg, err := loadConfig()
	if err != nil || cfg.SecretKey != "App_config1" || cfg.ManagedBy != "resolver-a" {
		t.Fatalf("unexpected config: %#v, %v", cfg, err)
	}
}

func TestLoadConfigAcceptsKubernetesSecretName(t *testing.T) {
	for _, value := range []string{"app-config", "app.config", "a", strings.Repeat("s", 253)} {
		t.Run(value, func(t *testing.T) {
			setValidConfig(t)
			t.Setenv("OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME", value)
			cfg, err := loadConfig()
			if err != nil || cfg.KubernetesSecretName != value {
				t.Fatalf("unexpected config: %#v, %v", cfg, err)
			}
		})
	}
}

func TestLoadConfigAcceptsNamespace(t *testing.T) {
	for key, value := range map[string]string{"OCI_VAULT_RESOLVER_REGION": "ap-tokyo-1", "OCI_VAULT_RESOLVER_COMPARTMENT_ID": "ocid1.compartment.oc1..example", "OCI_VAULT_RESOLVER_VAULT_NAME": "vault", "OCI_VAULT_RESOLVER_SECRET_NAME": "config", "OCI_VAULT_RESOLVER_SECRET_KEY": "config", "POD_NAMESPACE": "resolver-check"} {
		t.Setenv(key, value)
	}
	cfg, err := loadConfig()
	if err != nil || cfg.Namespace != "resolver-check" {
		t.Fatalf("unexpected config: %#v, %v", cfg, err)
	}
}

func TestLoadConfigAcceptsTenancyAsRootCompartment(t *testing.T) {
	for key, value := range map[string]string{"OCI_VAULT_RESOLVER_REGION": "ap-tokyo-1", "OCI_VAULT_RESOLVER_COMPARTMENT_ID": "ocid1.tenancy.oc1..example", "OCI_VAULT_RESOLVER_VAULT_NAME": "vault", "OCI_VAULT_RESOLVER_SECRET_NAME": "config", "OCI_VAULT_RESOLVER_SECRET_KEY": "config"} {
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
