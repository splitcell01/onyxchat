#!/usr/bin/env bash
set -euo pipefail

REGION="us-east-1"
ACCOUNT_ID="675950137665"
ECR_REPO="secure-messenger-backend"
ECR="${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com"
IMAGE_TAG="${1:-$(git rev-parse --short HEAD)}"
IMAGE_URI="${ECR}/${ECR_REPO}:${IMAGE_TAG}"

IAC_DIR="${HOME}/secure-messenger/iac/onyxchat-iac"
TFVARS="${IAC_DIR}/terraform.tfvars"

echo "==> Deploying ${IMAGE_URI}"

echo "==> ECR login"
aws ecr get-login-password --region "${REGION}" | docker login --username AWS --password-stdin "${ECR}"

echo "==> Build"
docker build -t "${IMAGE_URI}" .

echo "==> Push"
docker push "${IMAGE_URI}"

echo "==> Update terraform.tfvars image_uri"
# Replace existing image_uri line (or append if missing)
if grep -q '^image_uri' "${TFVARS}"; then
  sed -i "s|^image_uri *=.*|image_uri = \"${IMAGE_URI}\"|g" "${TFVARS}"
else
  echo "image_uri = \"${IMAGE_URI}\"" >> "${TFVARS}"
fi

echo "==> Terraform apply"
pushd "${IAC_DIR}" >/dev/null
terraform init -input=false
terraform apply -auto-approve
popd >/dev/null

echo "==> Wait for service steady state"
aws ecs wait services-stable \
  --region "${REGION}" \
  --cluster onyxchat-cluster \
  --services onyxchat-svc

echo "==> Smoke test readiness"
curl -fsS https://onyxchat.dev/health/ready | cat
echo
echo "done"
