# ADR 0003: kind and Terraform for the local bootstrap

- Status: Accepted
- Date: 2026-11-15

## Context

The project must be runnable on a laptop with one command while still demonstrating real infrastructure-as-code, a real GitOps controller, ingress, TLS, policy and monitoring. A cloud cluster would be more realistic but would add cost, credentials and a slow, environment-specific setup.

## Decision

Use Terraform with the `tehcyx/kind` provider to create a two-node kind cluster, and the Helm provider to install ArgoCD.

- One control-plane node and one worker; host ports 8080/8443 map to ingress NodePorts 30080/30443, so `*.localtest.me` reaches the ingress without `/etc/hosts` changes.
- A dedicated kubeconfig at `~/.kube/meridian` avoids clobbering the user's current context.
- Terraform installs ArgoCD, creates the `monitoring` namespace and the `grafana-admin` Secret, and applies the AppProject and root Application.
- The providers read the kubeconfig, which does not exist before the cluster is created. Bootstrap therefore applies the cluster target first, then the rest. `scripts/bootstrap.sh` encodes this ordering.
- Everything after ArgoCD is reconciled from Git, not Terraform.

## Consequences

- `make up` is reproducible and destroys cleanly with `make down`.
- The two-stage apply is a visible sharp edge. Without it, Terraform configures providers against a nonexistent kubeconfig. The script and README make the sequence explicit.
- Terraform manages the monitoring namespace and Grafana credential before the GitOps tree reaches that wave; ArgoCD adopts the namespace afterwards. This is a bootstrap exception, documented in the code.
- The local cluster is not highly available and does not model cloud networking, load balancers or node pools. Those are deliberately out of scope.
- Moving to a real cluster means replacing the kind provider with an EKS/GKE module and pointing the Helm/kubernetes providers at that cluster. The ArgoCD manifests, charts, policies and workflows do not change.

## Alternatives considered

- **kind CLI + shell script only** — simpler, but no infrastructure state, no provider-level lifecycle, and no demonstration of Terraform beyond a wrapper.
- **k3d/k3s** — faster startup, but kind is the reference implementation for upstream Kubernetes conformance and behaves more like a real cluster for admission webhooks and ingress.
- **Cloud cluster in the default path** — too costly and credential-heavy for a portfolio project. Provided as a documented migration.
