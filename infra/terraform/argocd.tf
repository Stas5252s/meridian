# ArgoCD is the only workload Terraform installs directly. Everything else
# (ingress, cert-manager, Kyverno, Rollouts, monitoring, the application) is
# reconciled from Git by the root Application, so the cluster converges to the
# repository rather than to Terraform state.
resource "helm_release" "argocd" {
  name             = "argocd"
  namespace        = var.argocd_namespace
  create_namespace = true
  repository       = "https://argoproj.github.io/argo-helm"
  chart            = "argo-cd"
  version          = var.argo_cd_chart_version
  timeout          = 900
  atomic           = true

  values = [
    yamlencode({
      global = {
        domain = "argocd.localtest.me"
      }
      configs = {
        params = {
          "server.insecure" = true
        }
      }
      dex = {
        enabled = false
      }
      notifications = {
        enabled = false
      }
      server = {
        service = {
          type = "ClusterIP"
        }
        resources = {
          requests = { cpu = "50m", memory = "128Mi" }
          limits   = { cpu = "500m", memory = "512Mi" }
        }
      }
      controller = {
        resources = {
          requests = { cpu = "100m", memory = "256Mi" }
          limits   = { cpu = "1", memory = "1Gi" }
        }
      }
      repoServer = {
        resources = {
          requests = { cpu = "50m", memory = "128Mi" }
          limits   = { cpu = "500m", memory = "512Mi" }
        }
      }
      applicationSet = {
        resources = {
          requests = { cpu = "25m", memory = "64Mi" }
          limits   = { cpu = "200m", memory = "256Mi" }
        }
      }
      redis = {
        resources = {
          requests = { cpu = "25m", memory = "64Mi" }
          limits   = { cpu = "200m", memory = "256Mi" }
        }
      }
    })
  ]

  depends_on = [kind_cluster.meridian]
}

# Terraform creates the monitoring namespace and Grafana credential before the
# GitOps tree reaches that sync wave. ArgoCD adopts the namespace afterwards.
resource "kubernetes_namespace_v1" "monitoring" {
  metadata {
    name = "monitoring"
    labels = {
      "app.kubernetes.io/part-of" = "meridian"
    }
  }

  depends_on = [kind_cluster.meridian]
}

resource "random_password" "grafana_admin" {
  length  = 24
  special = false
}

resource "kubernetes_secret_v1" "grafana_admin" {
  metadata {
    name      = "grafana-admin"
    namespace = kubernetes_namespace_v1.monitoring.metadata[0].name
  }

  data = {
    admin-user     = "admin"
    admin-password = random_password.grafana_admin.result
  }

  type = "Opaque"
}
