# kind publishes the ingress NodePorts on the host so localtest.me hostnames
# resolve to the cluster through the standard HTTP/HTTPS ports.
resource "kind_cluster" "meridian" {
  name            = var.cluster_name
  node_image      = var.node_image
  kubeconfig_path = pathexpand(var.kubeconfig_path)
  wait_for_ready  = true

  kind_config {
    kind        = "Cluster"
    api_version = "kind.x-k8s.io/v1alpha4"

    node {
      role = "control-plane"

      kubeadm_config_patches = [
        "kind: InitConfiguration\nnodeRegistration:\n  kubeletExtraArgs:\n    node-labels: \"ingress-ready=true\"\n"
      ]

      extra_port_mappings {
        container_port = 30080
        host_port      = 8080
        protocol       = "TCP"
      }

      extra_port_mappings {
        container_port = 30443
        host_port      = 8443
        protocol       = "TCP"
      }
    }

    node {
      role = "worker"
    }
  }
}
