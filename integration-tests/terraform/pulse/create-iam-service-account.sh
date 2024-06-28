#!/bin/bash

# Exit immediately if a command exits with a non-zero status
set -e

# Define variables
SERVICE_ACCOUNT_NAME=$1
NAMESPACE=$2
CLUSTER_NAME=$3
ROLE_NAME=$4
POLICY_ARN=$5
AWS_REGION=$6
AWS_ACCOUNT_ID=$7

# Retrieve the OIDC provider URL
OIDC_PROVIDER_URL=$(aws eks describe-cluster --name $CLUSTER_NAME --query "cluster.identity.oidc.issuer" --output text)

# Extract the OIDC ID
OIDC_ID=$(echo $OIDC_PROVIDER_URL | sed 's|https://oidc.eks\.[a-zA-Z0-9-]*\.amazonaws\.com/id/||')

# Create trust policy file
cat <<EOF > trust-policy.json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::$AWS_ACCOUNT_ID:oidc-provider/$OIDC_ID"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "$OIDC_ID:sub": "system:serviceaccount:$NAMESPACE:$SERVICE_ACCOUNT_NAME"
        }
      }
    }
  ]
}
EOF

# Create IAM role
aws iam create-role --role-name $ROLE_NAME --assume-role-policy-document file://trust-policy.json

# Attach policy to the IAM role
aws iam attach-role-policy --role-name $ROLE_NAME --policy-arn $POLICY_ARN

# Create service account YAML file
cat <<EOF > service-account.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: $SERVICE_ACCOUNT_NAME
  namespace: $NAMESPACE
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::$AWS_ACCOUNT_ID:role/$ROLE_NAME
EOF

# Apply service account
kubectl apply -f service-account.yaml
