#!/usr/bin/env bash
# 构建 3 个业务镜像并打 tag。可选 push 到 registry。
#
# 用法：
#   bash scripts/build-images.sh                        # 用默认 dev 版本，仅本机
#   INSPECTAI_VERSION=1.2.3 bash scripts/build-images.sh
#   REGISTRY=registry.cn-hangzhou.aliyuncs.com/myorg INSPECTAI_VERSION=1.2.3 \
#     bash scripts/build-images.sh --push
#
# Why: 没有显式 tag 的镜像不可回滚、不可灰度。git short-sha 是兜底版本号。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

REGISTRY="${REGISTRY:-inspectai}"
# 防呆：REGISTRY 不能以 / 结尾，会产生 //svc 这种坏镜像名
REGISTRY="${REGISTRY%/}"
if [[ -z "$REGISTRY" ]]; then
  echo "ERROR: REGISTRY 不能为空" >&2; exit 1
fi
# 默认用 git short-sha；不在 git 仓里时退到时间戳。
DEFAULT_VERSION="$(git -C "$ROOT_DIR/.." rev-parse --short HEAD 2>/dev/null || date +%Y%m%d-%H%M%S)"
INSPECTAI_VERSION="${INSPECTAI_VERSION:-$DEFAULT_VERSION}"

export REGISTRY INSPECTAI_VERSION

echo "Building images:"
echo "  REGISTRY=$REGISTRY"
echo "  INSPECTAI_VERSION=$INSPECTAI_VERSION"
echo

# 走 BuildKit，吃 .dockerignore + Dockerfile syntax=1.7 的 cache mount
export DOCKER_BUILDKIT=1

docker compose -f docker-compose.prod.yml build \
  ai-service go-backend admin-frontend

# 额外打 latest tag，方便本地 / 滚动部署引用最新
for svc in ai-service go-backend admin-frontend; do
  docker tag "${REGISTRY}/${svc}:${INSPECTAI_VERSION}" "${REGISTRY}/${svc}:latest"
done

echo
echo "Built tags:"
docker images --format '{{.Repository}}:{{.Tag}}\t{{.Size}}' \
  | grep -E "^${REGISTRY}/(ai-service|go-backend|admin-frontend)" \
  | sort

if [[ "${1:-}" == "--push" ]]; then
  echo
  echo "Pushing to registry..."
  for svc in ai-service go-backend admin-frontend; do
    docker push "${REGISTRY}/${svc}:${INSPECTAI_VERSION}"
    docker push "${REGISTRY}/${svc}:latest"
  done
fi
