# OCI Vault resolver

The resolver runs as a Kubernetes Job. In one run it copies one OCI Vault secret into one Kubernetes Secret: it finds exactly one active Vault with the configured display name, reads the current version of the named secret in that Vault, and writes the content unchanged under one key of the configured Kubernetes Secret in the Pod's namespace. The resolver does not parse the content; consumers read the key and decode it themselves. It does not watch the OCI secret, so run the Job again after the secret changes.

Configuration comes only from environment variables:

| Variable | Meaning |
| --- | --- |
| `OCI_VAULT_RESOLVER_REGION` | OCI region of the Vault. |
| `OCI_VAULT_RESOLVER_COMPARTMENT_ID` | Compartment (or tenancy) OCID to search for the Vault. |
| `OCI_VAULT_RESOLVER_VAULT_NAME` | Display name of the Vault. |
| `OCI_VAULT_RESOLVER_SECRET_NAME` | Name of the secret in the Vault. |
| `OCI_VAULT_RESOLVER_SECRET_KEY` | Key in the Kubernetes Secret's `data`. Starts with a letter and has only letters, digits and `_` (at most 253 characters), so Timoni runtime reads it as `obj.data.<key>` without quotes. |
| `OCI_VAULT_RESOLVER_MANAGED_BY` | Value of the managed-by annotation. Defaults to `oci-vault-resolver`. Give each resolver in a cluster its own value. |
| `OCI_VAULT_RESOLVER_KUBERNETES_SECRET_NAME` | Name of the Kubernetes Secret to write. Must be a valid Secret name (a DNS subdomain: lowercase letters, digits, `-` and `.`, starting and ending with a letter or digit, at most 253 characters). Defaults to `oci-vault-resolver`. Give each resolver in a namespace its own name. |
| `POD_NAMESPACE` | Namespace of the Kubernetes Secret, from the downward API. Defaults to `default`. |

`--version` prints the build version and target OS/architecture without reading configuration or initializing OCI/Kubernetes clients.

The resolver fails closed. When the Secret does not exist, it creates the Secret with the annotation `oci-vault-resolver.westelh.dev/managed-by` set to the configured value. An existing Secret is replaced whole (all its data, labels and annotations) only when that annotation has the configured value, it has no owner reference, its type is `Opaque`, and it is not immutable. Any other Secret fails the run without being changed; there is no setting to overwrite it. Writes are a create, which fails when the Secret already exists, or an update carrying the resourceVersion that was read. A concurrent change makes the run fail with `secret_write_conflict` (HTTP 409) instead of retrying, which is reported apart from `kubernetes_forbidden` (403) and `kubernetes_not_found` (404). Logs never include the secret content.

The resolver needs these permissions:

- OCI, through Instance Principal authentication: `inspect vaults` in the compartment, and `read secret-bundles` for the secret.
- Kubernetes, through the Pod's service account: `get`, `create` and `update` on `secrets` in the Pod's namespace. It uses no Lease or other resource.

## Development

Local checks need Go and [aqua](https://aquaproj.github.io/). Run `aqua i` to install [just](https://github.com/casey/just), [hk](https://github.com/jdx/hk) and [golangci-lint](https://golangci-lint.run/), then `hk install` once in each clone to enable the Git hooks. Run the checks with `hk check --all`. After changing a version in `aqua.yaml`, run `aqua upc -prune` to update `aqua-checksums.json`.

Resolver image builds and publishes run only in the GitHub Actions workflow. Pull requests build and start both Linux architectures from one OCI archive, and pushing a `vX.Y.Z` tag publishes that same archive to `ghcr.io/westelh/oci-vault-resolver`. Published tags are not recreated.
