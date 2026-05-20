#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: docker is not installed. Install Docker first." >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "ERROR: docker compose plugin is not available. Install Docker Compose v2 first." >&2
  exit 1
fi

if [ ! -f ".env.prod" ]; then
  cp ".env.prod.example" ".env.prod"
  echo "Created .env.prod from .env.prod.example."
  echo "Edit .env.prod, set MYSQL_PASSWORD, MYSQL_ROOT_PASSWORD, DASHSCOPE_API_KEY, INSPECTAI_AUTH_TOKEN, INSPECTAI_SUPERVISOR_TOKEN, and INSPECTAI_ADMIN_PASSWORD, then run this script again."
  exit 2
fi

if grep -Eq "change_me_|sk-your-dashscope-key" ".env.prod"; then
  echo "ERROR: .env.prod still contains placeholder secrets. Fill real values before deployment." >&2
  exit 3
fi

echo "[1/4] Build and start containers"
docker compose --env-file .env.prod -f docker-compose.prod.yml up -d --build

echo "[2/4] Wait for backend health"
for i in $(seq 1 60); do
  # 8080 is the backend's internal container port; local host access uses 18080.
  if docker compose --env-file .env.prod -f docker-compose.prod.yml exec -T go-backend wget -qO- http://127.0.0.1:8080/health 2>/dev/null | grep -q '"status":"ok"'; then
    break
  fi
  sleep 2
  if [ "$i" -eq 60 ]; then
    echo "ERROR: backend health check timeout." >&2
    docker compose --env-file .env.prod -f docker-compose.prod.yml logs --tail=120 go-backend ai-service mysql
    exit 4
  fi
done

echo "[3/4] Service status"
docker compose --env-file .env.prod -f docker-compose.prod.yml ps

echo "[4/4] Done"
echo "Open: http://SERVER_IP/"
echo "Backend health: http://SERVER_IP/health"
