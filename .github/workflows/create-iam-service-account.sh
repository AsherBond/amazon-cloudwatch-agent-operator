#!/bin/bash

# Variables
CLUSTER_NAME="cw-agent-eks-addon-test-beta-cluster"
REGION="us-west-2"
ACCOUNT_ID=$(aws sts get-caller-identity --query "Account" --output text)
OIDC_PROVIDER=$(aws eks describe-cluster --name $CLUSTER_NAME --region $REGION --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")
SAMPLE_APP_NAMESPACE="sample-app-namespace"
TESTING_ID=env.TESTING_ID

# Create trust policy JSON file
cat <<EOF > trust-policy.json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::$ACCOUNT_ID:oidc-provider/$OIDC_PROVIDER"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "$OIDC_PROVIDER:sub": "system:serviceaccount:$SAMPLE_APP_NAMESPACE:service-account-$TESTING_ID"
        }
      }
    }
  ]
}
EOF

# Create IAM role
aws iam create-role --role-name eks-s3-access-$TESTING_ID --assume-role-policy-document file://trust-policy.json

# Attach policy to the role
aws iam attach-role-policy --role-name eks-s3-access-$TESTING_ID --policy-arn arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess

# Create service account YAML manifest
cat <<EOF > service-account.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: service-account-$TESTING_ID
  namespace: $SAMPLE_APP_NAMESPACE
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::$ACCOUNT_ID:role/eks-s3-access-$TESTING_ID
EOF

# Apply the service account
kubectl apply -f service-account.yaml

echo "IAM service account created and configured successfully."
