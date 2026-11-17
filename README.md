# OCI Vault resolver

The resolver runs as the fixed `default/platform-vault-resolver` Job and is configured only through the `OCI_MANIFEST_*` variables (`REGION`, `COMPARTMENT_ID`, `VAULT_NAME`, `SECRET_NAME`) and the downward API `POD_UID`. It uses OCI Instance Principal authentication, lists vault metadata only, and creates or updates the fixed `default/platform-manifest` SecretStore after finding exactly one active Vault with the configured display name.

`--version` prints the build version and target OS/architecture without reading configuration or initializing OCI/Kubernetes clients.

An existing SecretStore is updated only when its resolver ownership marker is present, it has no owner reference, it uses only the Oracle provider, and its current region, Vault OCID, and compartment OCID are all explicit and match the resolver's resolved target. `principalType` may be omitted or set to `InstancePrincipal`. Missing, malformed, or different values fail closed without changing the Store. In particular, the resolver does not fill in a missing compartment or adopt a Store that points at another Vault. An operator must inspect and repair the Store before retrying.

Run unit tests from the repository root with `just test`. Resolver image builds and publishes run only in the GitHub Actions workflow; pull requests build and start both Linux architectures from the same OCI archive before any publish job can run.
