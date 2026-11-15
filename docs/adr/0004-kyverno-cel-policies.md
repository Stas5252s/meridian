# ADR 0004: Kyverno CEL policies instead of legacy ClusterPolicy

- Status: Accepted
- Date: 2026-11-15

## Context

The platform needs admission policy for workload hardening, ownership metadata, image tags and registries, plus keyless signature and SBOM verification for production images. The original implementation used `kyverno.io/v1` ClusterPolicies, which were the standard for years.

Kyverno 1.20 (the version shipped by Helm chart 3.10.0, pinned in this repository) removed execution of legacy `kyverno.io` policy types. The API server still accepts reads and deletes, but the admission controller no longer evaluates them. A legacy policy suite would install cleanly and silently enforce nothing.

## Decision

Use the CEL-based `policies.kyverno.io/v1` APIs:

- `ValidatingPolicy` for probes, resource limits, mutable tags, ownership labels, container security and allowed registries.
- `MutatingPolicy` to add an environment label to pods, because pods do not inherit namespace labels.
- `ImageValidatingPolicy` for keyless cosign signature and SPDX attestation verification in production, with digest mutation at admission.

Namespace scoping is expressed with a CEL `matchConditions` expression on `object.metadata.namespace` rather than a `namespaceSelector`. This keeps policies precise while remaining testable with the Kyverno CLI without a live cluster (the CLI does not resolve namespace labels in unit tests).

Policies are unit tested with `kyverno test`:

- 24 validation assertions across good/bad pods and Deployments,
- a mutation test that compares the patched pod,
- signature verification is not unit tested because it requires a registry and Sigstore network access; it is covered by the release workflow's `cosign verify` and by production admission.

## Consequences

- The policy suite actually runs on Kyverno 1.20 and is CEL-native, which aligns with the upstream direction and avoids a future migration.
- CEL expressions are more verbose than YAML patterns and require `has()` guards for optional fields.
- Audit policies report violations without blocking; production security depends on the `restricted` Pod Security Standard, the enforce-mode registry and image policies, and the canary gate.
- `ImageValidatingPolicy` fails closed. A Sigstore/Rekor outage blocks production admission, which is the correct trust trade-off for production but should be understood before enabling it in a cluster without a signature mirror.
- The keyless subject is repository-specific. `scripts/set-github-org.sh` rewrites the image glob, issuer subject and registry paths for a fork.

## Alternatives considered

- **Pin Kyverno < 1.20 to keep ClusterPolicy** — delays a hard migration, loses upstream CEL validation, and pins the platform to an unsupported execution model. Rejected.
- **Gatekeeper/OPA** — capable, but adds a second policy language and does not provide native image signature verification.
- **Pod Security Admission only** — covers the security baseline but cannot express registry allow-lists, signatures, SBOM attestations or ownership labels.
