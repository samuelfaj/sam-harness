#!/bin/sh
set -eu

SAM_HARNESS_REPO_ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
SAM_HARNESS_TEMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/sam-harness-e2e.XXXXXX")"
trap 'rm -rf "$SAM_HARNESS_TEMP_ROOT"' EXIT HUP INT TERM

SAM_HARNESS_FIXTURE="$SAM_HARNESS_TEMP_ROOT/repository"
SAM_HARNESS_BIN="$SAM_HARNESS_TEMP_ROOT/sam-harness"
SAM_HARNESS_PLAN="$SAM_HARNESS_TEMP_ROOT/plan.json"
SAM_HARNESS_SECOND_PLAN="$SAM_HARNESS_TEMP_ROOT/plan-second.json"

cp -R "$SAM_HARNESS_REPO_ROOT/testdata/fixtures/full-flow" "$SAM_HARNESS_FIXTURE"
git -C "$SAM_HARNESS_FIXTURE" init -q
git -C "$SAM_HARNESS_FIXTURE" add .
git -C "$SAM_HARNESS_FIXTURE" -c user.name=sam-harness -c user.email=sam-harness@example.invalid commit -qm fixture

go build -o "$SAM_HARNESS_BIN" ./cmd/sam-harness

"$SAM_HARNESS_BIN" plan "$SAM_HARNESS_FIXTURE" \
  --profile production \
  --answers "$SAM_HARNESS_FIXTURE/answers.production.json" \
  --output "$SAM_HARNESS_PLAN"

SAM_HARNESS_PLAN_ID="$(sed -n 's/^[[:space:]]*"id": "\([^"]*\)",*/\1/p' "$SAM_HARNESS_PLAN" | head -n 1)"
test -n "$SAM_HARNESS_PLAN_ID"
"$SAM_HARNESS_BIN" apply --plan "$SAM_HARNESS_PLAN" --accept "$SAM_HARNESS_PLAN_ID"

test -f "$SAM_HARNESS_FIXTURE/.sam-harness/WORKFLOW.md"
test -f "$SAM_HARNESS_FIXTURE/.sam-harness/REVIEWERS.md"
test -f "$SAM_HARNESS_FIXTURE/.sam-harness/CHANGE_BUDGET.md"
test -f "$SAM_HARNESS_FIXTURE/.sam-harness/runbooks/observability.md"
test -f "$SAM_HARNESS_FIXTURE/.sam-harness/runbooks/retirement.md"
test -f "$SAM_HARNESS_FIXTURE/.github/workflows/sam-harness.yml"
test -f "$SAM_HARNESS_FIXTURE/.sam-harness/ci/gitlab.yml"

"$SAM_HARNESS_BIN" doctor "$SAM_HARNESS_FIXTURE"

for SAM_HARNESS_PHASE in static test review artifact staging production observe migration rollback; do
  "$SAM_HARNESS_BIN" pipeline "$SAM_HARNESS_FIXTURE" --phase "$SAM_HARNESS_PHASE" --receipt true
done

"$SAM_HARNESS_BIN" plan "$SAM_HARNESS_FIXTURE" \
  --profile production \
  --answers "$SAM_HARNESS_FIXTURE/answers.production.json" \
  --output "$SAM_HARNESS_SECOND_PLAN"

SAM_HARNESS_SECOND_PLAN_ID="$(sed -n 's/^[[:space:]]*"id": "\([^"]*\)",*/\1/p' "$SAM_HARNESS_SECOND_PLAN" | head -n 1)"
test -n "$SAM_HARNESS_SECOND_PLAN_ID"
SAM_HARNESS_SECOND_OUTPUT="$SAM_HARNESS_TEMP_ROOT/second-apply.txt"
"$SAM_HARNESS_BIN" apply --plan "$SAM_HARNESS_SECOND_PLAN" --accept "$SAM_HARNESS_SECOND_PLAN_ID" >"$SAM_HARNESS_SECOND_OUTPUT"
grep -q "No files changed" "$SAM_HARNESS_SECOND_OUTPUT"

test -n "$(find "$SAM_HARNESS_FIXTURE/.sam-harness/evidence" -type f -name '*.json' -print -quit)"
printf '%s\n' "full-flow e2e: PASS"
