variable "cluster_name" {
  description = "kind cluster name."
  type        = string
  default     = "meridian"
}

variable "kubeconfig_path" {
  description = "Dedicated kubeconfig written by the kind provider."
  type        = string
  default     = "~/.kube/meridian"
}

variable "node_image" {
  description = "kindest/node image (pin to a supported Kubernetes release)."
  type        = string
  default     = "kindest/node:v1.34.0"
}

variable "gitops_repo_url" {
  description = "Git repository ArgoCD watches. Replace the placeholder with your fork."
  type        = string
  default     = "https://github.com/your-org/meridian.git"
}

variable "argo_cd_chart_version" {
  description = "argo-cd Helm chart version installed into the cluster."
  type        = string
  default     = "10.18.0"
}

variable "argocd_namespace" {
  description = "Namespace for the ArgoCD control plane."
  type        = string
  default     = "argocd"
}
