#!/usr/bin/env bash
# Run only in an isolated Actions job WITHOUT the publication credential.
set -euo pipefail

candidate=${1:?Usage: verify.sh CANDIDATE_DIRECTORY}
cd "$candidate"
test "${GITHUB_ACTIONS:-}" = true || {
  echo 'This gate requires an isolated GitHub Actions runner and disposable services.' >&2
  exit 1
}
test -z "${PUBLISH_TOKEN:-}" || {
  echo 'Never expose the publisher credential to candidate code.' >&2
  exit 1
}
export CI=true
task_tmp=$(mktemp -d "${RUNNER_TEMP:?}/pr163-gate.XXXXXX")
mkdir -p "$task_tmp/bin"
export PATH="$task_tmp/bin:$PATH"
pids=()
cleanup() {
  local pid
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  for pid in "${pids[@]}"; do wait "$pid" 2>/dev/null || true; done
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

GOWORK=off GOBIN="$task_tmp/bin" go install github.com/swaggo/swag/cmd/swag@v1.16.6
GOWORK=off GOBIN="$task_tmp/bin" go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1
GOWORK=off GOBIN="$task_tmp/bin" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GOWORK=off GOBIN="$task_tmp/bin" go install golang.org/x/tools/cmd/goimports@v0.48.0
# Check before npm vendors unrelated third-party Go sources under node_modules.
make format-check-go
make swagger-contract-check
if rg -q 'validate-compose:' .github/workflows/ci.yml; then
  for file in docker-compose.yml docker-compose.dev.yml .devcontainer/docker-compose.yml; do
    DB_PASSWORD=ci-only REDIS_PASSWORD=ci-only SELFHOSTED_IMAGE=ghcr.io/when-to/whento:ci-placeholder \
      docker compose -f "$file" config -q
  done
  # PR 180's deployment contract must remain true in every later topic.
  python3 scripts/check-bootstrap-compose.py
fi
(
  cd frontend
  npm ci
  npm run type-check
  npm run lint
  npm run format:check
  npm run test:coverage
  for seed in 1 42 163 2026; do
    npm run test -- --sequence.shuffle --sequence.seed="$seed"
  done
  npm run build:cloud
  docker build --progress plain -f ../build/cloud/Dockerfile -t whento-pr163-test:cloud ..
  npm run build:selfhosted
  docker build --progress plain -f ../build/selfhosted/Dockerfile -t whento-pr163-test:selfhosted ..
)
make types-check
mkdir -p web/dist
cp -a frontend/dist/. web/dist/
make lint-go

# The only server is this job's fresh service. Never accept user DB settings.
db_admin='postgres://test:test@127.0.0.1:5432/postgres?sslmode=disable'
export REDIS_URL=redis://127.0.0.1:6379
for variant in selfhosted cloud; do
  database="pr163_$variant"
  psql "$db_admin" -v ON_ERROR_STOP=1 -qc "CREATE DATABASE $database"
  export DATABASE_URL="postgres://test:test@127.0.0.1:5432/$database?sslmode=disable"
  chain="$task_tmp/migrations-$variant"
  mkdir "$chain"
  cp migrations/common/*.sql migrations/"$variant"/*.sql "$chain/"
  migrate -path "$chain" -database "$DATABASE_URL" up
  mapfile -t packages < <(go list -tags "$variant" ./... | rg -v '/node_modules/')
  test "${#packages[@]}" -gt 0
  go test -tags "$variant" -race -count=1 -covermode=atomic -coverprofile="$task_tmp/$variant-root.out" "${packages[@]}"
  (cd pkg && go test -tags "$variant" -race -count=1 -covermode=atomic -coverprofile="$task_tmp/$variant-pkg.out" ./...)
  go build -tags "$variant" -o "$task_tmp/whento-$variant" ./cmd/
done
for module in root pkg; do
  floor=47
  test "$module" != pkg || floor=78
  total=$(go tool cover -func="$task_tmp/selfhosted-$module.out" | awk '/^total:/ {gsub(/%/, "", $NF); print $NF}')
  test -n "$total"
  awk -v total="$total" -v floor="$floor" 'BEGIN {exit total < floor}'
done

if test -f scripts/test-migrations.sh; then
  DISPOSABLE_DB_URL="$db_admin" MIGRATE_BIN="$task_tmp/bin/migrate" bash scripts/test-migrations.sh
  shellcheck scripts/migrate.sh scripts/init-db.sh scripts/build-migrations.sh scripts/migration-common.sh
fi

(
  cd frontend
  npx playwright install --with-deps chromium
  npm run test:e2e
)
psql "$db_admin" -v ON_ERROR_STOP=1 -qc 'CREATE DATABASE pr163_e2e'
export DATABASE_URL='postgres://test:test@127.0.0.1:5432/pr163_e2e?sslmode=disable'
migrate -path "$task_tmp/migrations-selfhosted" -database "$DATABASE_URL" up
export BOOTSTRAP_KEY=ci-only-bootstrap-key-not-for-production
export RATE_LIMIT_ENABLED=false
export APP_URL=http://127.0.0.1:8080
export WHENTO_ROTATION_BASE_URL=http://127.0.0.1:5174
export WHENTO_ROTATION_API=http://127.0.0.1:5174/api/v1
bash scripts/generate-keys.sh
PORT=5173 "$task_tmp/whento-selfhosted" > "$task_tmp/backend.log" 2>&1 &
pids+=("$!")
PORT=5174 JWT_ACCESS_EXPIRY=2m APP_URL=http://127.0.0.1:5174 \
  "$task_tmp/whento-selfhosted" > "$task_tmp/rotation.log" 2>&1 &
pids+=("$!")
(
  cd frontend
  # Track Vite itself, not npm's wrapper, so cleanup stops the actual server.
  VITE_BUILD_TYPE=selfhosted exec ./node_modules/.bin/vite --host 127.0.0.1
) > "$task_tmp/frontend.log" 2>&1 &
pids+=("$!")
wait_healthy() {
  local pid=$1 url=$2
  for _ in {1..60}; do
    kill -0 "$pid" 2>/dev/null || { echo "Server exited before $url became healthy" >&2; return 1; }
    if curl -fsS "$url" > /dev/null 2>&1; then return 0; fi
    sleep 2
  done
  echo "Timed out waiting for $url" >&2
  return 1
}
wait_healthy "${pids[0]}" http://127.0.0.1:5173/api/health
wait_healthy "${pids[1]}" http://127.0.0.1:5174/api/health
wait_healthy "${pids[2]}" http://127.0.0.1:8080/
(cd frontend && npm run test:e2e:backend)
docker build --progress plain -f Dockerfile -t whento-pr163-test:standalone .
echo 'All candidate verification gates passed.'
