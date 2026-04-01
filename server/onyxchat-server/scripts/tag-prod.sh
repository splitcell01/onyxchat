#!/usr/bin/env bash
set -euo pipefail

REGION="us-east-1"
ECR_REPO="secure-messenger-backend"
IMAGE_TAG="${1:?SHA tag required}"

MANIFEST="$(aws ecr batch-get-image \
  --region "${REGION}" \
  --repository-name "${ECR_REPO}" \
  --image-ids imageTag="${IMAGE_TAG}" \
  --query 'images[0].imageManifest' \
  --output text)"

OUT="$(aws ecr put-image \
  --region "${REGION}" \
  --repository-name "${ECR_REPO}" \
  --image-tag prod \
  --image-manifest "${MANIFEST}" 2>&1)" || {

  if echo "$OUT" | grep -q ImageAlreadyExistsException; then
    echo "prod tag already set (ok)"
  else
    echo "$OUT" >&2
    exit 1
  fi
}

echo "✅ prod tag applied"
