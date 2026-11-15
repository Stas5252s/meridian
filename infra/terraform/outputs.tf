output "cluster_name" {
  value = kind_cluster.meridian.name
}

output "kubeconfig_path" {
  value = kind_cluster.meridian.kubeconfig_path
}

output "api_endpoint" {
  value = kind_cluster.meridian.endpoint
}

output "argocd_port_forward" {
  description = "Run this to open the ArgoCD UI."
  value       = "kubectl -n ${var.argocd_namespace} port-forward svc/argocd-server 8081:80"
}

output "argocd_admin_password_command" {
  value = "kubectl -n ${var.argocd_namespace} get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d"
}

output "grafana_url" {
  value = "https://grafana.localtest.me:8443"
}

output "grafana_admin_password" {
  value     = random_password.grafana_admin.result
  sensitive = true
}

output "checkout_urls" {
  value = {
    local   = "https://checkout.localtest.me:8443"
    staging = "https://checkout-staging.localtest.me:8443"
    prod    = "https://checkout-prod.localtest.me:8443"
  }
}
