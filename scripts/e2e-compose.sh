#!/usr/bin/env bash
# End to end proof of issue 149 against the compose stack: builds and starts
# docker-compose.yaml with the docker-compose.e2e.yaml overlay, waits for the
# frontend on http://localhost:8080, then runs internal/e2e: login,
# GET /api/v1/me and a CSRF protected POST, all through the frontend's one
# origin. Reads POSTGRES_SUPERUSER_PASSWORD from the environment or .env,
# never prints it. Leaves the stack running; "task dev:down" stops it.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ -z "${POSTGRES_SUPERUSER_PASSWORD:-}" ] && [ -f .env ]; then
  # shellcheck disable=SC1091
  set -a && . ./.env && set +a
fi
: "${POSTGRES_SUPERUSER_PASSWORD:?set it in .env, see .env.example}"

compose=(docker compose -f docker-compose.yaml -f docker-compose.e2e.yaml)
"${compose[@]}" up --build -d

base="${E2E_BASE_URL:-http://localhost:8080}"
for _ in $(seq 1 120); do
  if curl -fsS -o /dev/null "${base}/api/v1/auth/methods"; then
    break
  fi
  sleep 1
done
curl -fsS -o /dev/null "${base}/api/v1/auth/methods" || {
  echo "the stack did not answer ${base}/api/v1/auth/methods within 120 seconds" >&2
  "${compose[@]}" ps >&2
  exit 1
}

E2E_BASE_URL="${base}" \
  E2E_DATABASE_URL="postgres://solobill_super:${POSTGRES_SUPERUSER_PASSWORD}@127.0.0.1:55432/solobill?sslmode=disable" \
  go test -tags=e2e -count=1 -v ./internal/e2e/...
