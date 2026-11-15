terraform {
  required_version = ">= 1.9.0"

  required_providers {
    kind = {
      source  = "tehcyx/kind"
      version = "~> 0.13"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 3.3"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 3.5"
    }
    kubectl = {
      source  = "alekc/kubectl"
      version = "~> 3.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.9"
    }
  }
}
