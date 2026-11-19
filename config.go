package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

var ErrInvalidConfig = errors.New("invalid resolver configuration")
var regionPattern = regexp.MustCompile(`^[a-z0-9-]+$`)
var namespacePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// A Secret data key that Timoni runtime can read as obj.data.<key> without
// quotes: a dot makes a nested path, "-" a subtraction, a leading "_" a hidden
// field and a leading digit a number.
var secretKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,252}$`)

type Config struct {
	Namespace            string
	Region               string
	CompartmentID        string
	VaultName            string
	SecretName           string
	SecretKey            string
	ManagedBy            string
	KubernetesSecretName string
}

// These names are the resolver's configuration interface; renaming one is a
// breaking change for every deployment that sets it.
const (
	envRegion               = "OCI_VAULT_RESOLVER_REGION"
	envCompartmentID        = "OCI_VAULT_RESOLVER_COMPARTMENT_ID"
	envVaultName            = "OCI_VAULT_RESOLVER_VAULT_NAME"
	envSecretName           = "OCI_VAULT_RESOLVER_SECRET_NAME"
	envSecretKey            = "OCI_VAULT_RESOLVER_SECRET_KEY"
	envManagedBy            = "OCI_VAULT_RESOLVER_MANAGED_BY"
	envKubernetesSecretName = "OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME"
)

func loadConfig() (Config, error) {
	keys := []string{envCompartmentID, envRegion, envSecretName, envSecretKey, envVaultName, envManagedBy, envKubernetesSecretName, "POD_NAMESPACE"}
	values := make(map[string]string, len(keys))
	// Values are used as given, without trimming, so the Vault and the Secret
	// are looked up by exactly the names they were created with in OCI.
	for _, key := range keys {
		value := os.Getenv(key)
		if key == "POD_NAMESPACE" && value == "" {
			value = "default"
		}
		if key == envManagedBy && value == "" {
			value = defaultManagedBy
		}
		if key == envKubernetesSecretName && value == "" {
			value = defaultKubernetesSecretName
		}
		if value == "" {
			return Config{}, fmt.Errorf("%w: %s is required", ErrInvalidConfig, key)
		}
		values[key] = value
	}
	// The Vault and Secret names are left to OCI to judge, so any name OCI
	// accepts can be configured. The checks below only cover values that the
	// resolver passes on as identifiers or keys.
	for _, key := range keys {
		value := values[key]
		switch key {
		case envRegion:
			if !regionPattern.MatchString(value) {
				return Config{}, fmt.Errorf("%w: %s has invalid format", ErrInvalidConfig, key)
			}
		case envCompartmentID:
			if !strings.HasPrefix(value, "ocid1.compartment.") && !strings.HasPrefix(value, "ocid1.tenancy.") {
				return Config{}, fmt.Errorf("%w: %s has invalid format", ErrInvalidConfig, key)
			}
		case envSecretKey:
			if !secretKeyPattern.MatchString(value) {
				return Config{}, fmt.Errorf("%w: %s has invalid format", ErrInvalidConfig, key)
			}
		case envKubernetesSecretName:
			if len(validation.IsDNS1123Subdomain(value)) != 0 {
				return Config{}, fmt.Errorf("%w: %s has invalid format", ErrInvalidConfig, key)
			}
		}
	}
	if !namespacePattern.MatchString(values["POD_NAMESPACE"]) {
		return Config{}, fmt.Errorf("%w: POD_NAMESPACE has invalid format", ErrInvalidConfig)
	}
	return Config{
		Namespace: values["POD_NAMESPACE"],
		Region:    values[envRegion], CompartmentID: values[envCompartmentID],
		VaultName: values[envVaultName], SecretName: values[envSecretName],
		SecretKey: values[envSecretKey], ManagedBy: values[envManagedBy],
		KubernetesSecretName: values[envKubernetesSecretName],
	}, nil
}
