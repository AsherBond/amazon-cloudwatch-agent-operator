// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT

module "common" {
  source = "../common"
}

module "basic_components" {
  source = "../basic_components"
}

locals {
  aws_eks  = "aws eks --region ${var.region}"
  cluster_name = var.cluster_name != "" ? var.cluster_name : "cwagent-operator-helm-integ"
}

data "aws_eks_cluster_auth" "this" {
  name = aws_eks_cluster.this.name
}

data "aws_caller_identity" "account_id" {}

data "aws_eks_cluster" "eks_windows_cluster_ca" {
  name = aws_eks_cluster.this.name
}

output "account_id" {
  value = data.aws_caller_identity.account_id.account_id
}

resource "aws_eks_cluster" "this" {
  name     = "${local.cluster_name}-${module.common.testing_id}"
  role_arn = module.basic_components.role_arn
  version  = var.k8s_version
  vpc_config {
    subnet_ids         = module.basic_components.public_subnet_ids
    security_group_ids = [module.basic_components.security_group]
  }
}

## EKS Cluster Addon

resource "aws_eks_addon" "eks_windows_addon" {
  cluster_name = aws_eks_cluster.this.name
  addon_name   = "vpc-cni"
}

## Enable VPC CNI Windows Support

resource "kubernetes_config_map_v1_data" "amazon_vpc_cni_windows" {
  depends_on = [
    aws_eks_cluster.this,
    aws_eks_addon.eks_windows_addon
  ]
  metadata {
    name      = "amazon-vpc-cni"
    namespace = "kube-system"
  }

  force = true

  data = {
    enable-windows-ipam : "true"
  }
}

## AWS CONFIGMAP

resource "kubernetes_config_map" "configmap" {
  data = {
    "mapRoles" = <<EOT
- groups:
  - system:bootstrappers
  - system:nodes
  rolearn: arn:aws:iam::${data.aws_caller_identity.account_id.account_id}:role/${local.cluster_name}-Worker-Role-${module.common.testing_id}
  username: system:node:{{EC2PrivateDNSName}}
- groups:
  - eks:kube-proxy-windows
  - system:bootstrappers
  - system:nodes
  rolearn: arn:aws:iam::${data.aws_caller_identity.account_id.account_id}:role/${local.cluster_name}-Worker-Role-${module.common.testing_id}
  username: system:node:{{EC2PrivateDNSName}}
- groups:
  - system:masters
  rolearn: arn:aws:iam::${data.aws_caller_identity.account_id.account_id}:role/Admin-Windows
EOT
  }

  metadata {
    name      = "aws-auth"
    namespace = "kube-system"
  }
}

# EKS Node Groups
resource "aws_eks_node_group" "this" {
  cluster_name    = aws_eks_cluster.this.name
  node_group_name = "${local.cluster_name}-node"
  node_role_arn   = aws_iam_role.node_role.arn
  subnet_ids      = module.basic_components.public_subnet_ids

  scaling_config {
    desired_size = 1
    max_size     = 1
    min_size     = 1
  }

  ami_type       = "AL2_x86_64"
  capacity_type  = "ON_DEMAND"
  disk_size      = 20
  instance_types = ["t3a.medium"]

  depends_on = [
    aws_iam_role_policy_attachment.node_CloudWatchAgentServerPolicy,
    aws_iam_role_policy_attachment.node_AmazonEC2ContainerRegistryReadOnly,
    aws_iam_role_policy_attachment.node_AmazonEKS_CNI_Policy,
    aws_iam_role_policy_attachment.node_AmazonEKSWorkerNodePolicy
  ]
}

# EKS Windows Node Groups
resource "aws_eks_node_group" "node_group_windows" {
  cluster_name    = aws_eks_cluster.this.name
  node_group_name = "${local.cluster_name}-windows-node"
  node_role_arn   = aws_iam_role.node_role.arn
  subnet_ids      = module.basic_components.public_subnet_ids

  scaling_config {
    desired_size = 1
    max_size     = 1
    min_size     = 1
  }

  ami_type       = "WINDOWS_CORE_2022_x86_64"
  capacity_type  = "ON_DEMAND"
  disk_size      = 50
  instance_types = ["t3a.medium"]

  depends_on = [
    aws_iam_role_policy_attachment.node_CloudWatchAgentServerPolicy,
    aws_iam_role_policy_attachment.node_AmazonEC2ContainerRegistryReadOnly,
    aws_iam_role_policy_attachment.node_AmazonEKS_CNI_Policy,
    aws_iam_role_policy_attachment.node_AmazonEKSWorkerNodePolicy
  ]
}

# EKS Node IAM Role
resource "aws_iam_role" "node_role" {
  name = "${local.cluster_name}-Worker-Role-${module.common.testing_id}"

  assume_role_policy = <<POLICY
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "ec2.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
POLICY
}

resource "aws_iam_role_policy_attachment" "node_AmazonEKSWorkerNodePolicy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy"
  role       = aws_iam_role.node_role.name
}

resource "aws_iam_role_policy_attachment" "node_AmazonEKS_CNI_Policy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"
  role       = aws_iam_role.node_role.name
}

resource "aws_iam_role_policy_attachment" "node_AmazonEC2ContainerRegistryReadOnly" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
  role       = aws_iam_role.node_role.name
}

resource "aws_iam_role_policy_attachment" "node_CloudWatchAgentServerPolicy" {
  policy_arn = "arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"
  role       = aws_iam_role.node_role.name
}

resource "null_resource" "kubectl" {
  depends_on = [
    aws_eks_cluster.this,
    aws_eks_node_group.this,
    aws_eks_node_group.node_group_windows
  ]
  provisioner "local-exec" {
    command = <<-EOT
      ${local.aws_eks} update-kubeconfig --name ${aws_eks_cluster.this.name}
      ${local.aws_eks} list-clusters --output text
      ${local.aws_eks} describe-cluster --name ${aws_eks_cluster.this.name} --output text
    EOT
  }
}

data "kubernetes_nodes" "debug3" {
  depends_on = [
    null_resource.kubectl
  ]
}

output "node-ids" {
  value = [for node in data.kubernetes_nodes.debug3.nodes : node.spec.0.provider_id]
}

data "kubernetes_config_map" "debug1" {
  depends_on = [
    aws_eks_cluster.this,
    aws_eks_node_group.this,
    aws_eks_node_group.node_group_windows
  ]
  metadata {
    name      = "aws-auth"
    namespace = "kube-system"
  }
}

output "cm-debug1" {
  depends_on = [
    data.kubernetes_config_map.debug1
  ]
  value = data.kubernetes_config_map.debug1.data
}

resource "helm_release" "this" {
  depends_on = [
    null_resource.kubectl
  ]
  name = "amazon-cloudwatch-observability"
  namespace = "amazon-cloudwatch"
  create_namespace = true
  chart      = "${var.helm_dir}"
}

resource "time_sleep" "wait_7_min" {
  depends_on = [helm_release.this]

  create_duration = "7m"
}

data "kubernetes_pod" "debug2" {
  depends_on = [
    time_sleep.wait_7_min
  ]
  metadata {
    name = "cloudwatch"
    namespace = "amazon-cloudwatch"
  }
}

#output "pod-debug2" {
#  value = data.kubernetes_pod.debug2.status
#}

resource "null_resource" "validator" {
  depends_on = [
    helm_release.this,
    time_sleep.wait_7_min
  ]
  provisioner "local-exec" {
    command = "go test ${var.test_dir} -v --tags=windowslinux"
  }
}
