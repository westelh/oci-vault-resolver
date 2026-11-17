package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var ErrInvalidConfig = errors.New("invalid resolver configuration")
var regionPattern = regexp.MustCompile(`^[a-z0-9-]+$`)
var namespacePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type Config struct {
	Namespace     string
	Region        string
	CompartmentID string
	VaultName     string
	SecretName    string
	PodUID        string
}

// Keep in sync with the env in cluster/bootstrap/platform/vault-resolver/templates/job.cue.
const (
	envRegion        = "OCI_MANIFEST_REGION"
	envCompartmentID = "OCI_MANIFEST_COMPARTMENT_ID"
	envVaultName     = "OCI_MANIFEST_VAULT_NAME"
	envSecretName    = "OCI_MANIFEST_SECRET_NAME"
)

func loadConfig() (Config, error) {
	keys := []string{envCompartmentID, envRegion, envSecretName, envVaultName, "POD_NAMESPACE", "POD_UID"}
	values := make(map[string]string, len(keys))
	// Values are used as given, without trimming, so the Vault is looked up by
	// exactly the name that cluster/bootstrap/bundle.cue checked and Terraform created.
	for _, key := range keys {
		value := os.Getenv(key)
		if key == "POD_NAMESPACE" && value == "" {
			value = "default"
		}
		if value == "" {
			return Config{}, fmt.Errorf("%w: %s is required", ErrInvalidConfig, key)
		}
		values[key] = value
	}
	// Keep looser than the format rules in cluster/bootstrap/bundle.cue; these
	// only keep the region and compartment ID safe to pass to the OCI SDK.
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
		case "POD_UID":
			if len(value) > 128 {
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
		VaultName: values[envVaultName], SecretName: values[envSecretName], PodUID: values["POD_UID"],
	}, nil
}
