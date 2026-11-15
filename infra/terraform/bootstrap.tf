locals {
  app_project_yaml = replace(
    file("${path.module}/../../deploy/gitops/projects/meridian.yaml"),
    "https://github.com/Stas5252s/meridian.git",
    var.gitops_repo_url
  )
  root_app_yaml = replace(
    file("${path.module}/../../deploy/gitops/bootstrap/root-app.yaml"),
    "https://github.com/Stas5252s/meridian.git",
    var.gitops_repo_url
  )
}

resource "kubectl_manifest" "app_project" {
  yaml_body = local.app_project_yaml

  depends_on = [helm_release.argocd]
}

resource "kubectl_manifest" "root_app" {
  yaml_body = local.root_app_yaml

  depends_on = [kubectl_manifest.app_project]
}
