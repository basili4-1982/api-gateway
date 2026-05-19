#!/bin/bash
# Lint-to-issues: run golangci-lint and create/update GitHub issue with results.
# Usage: ./scripts/lint-to-issues.sh [--dry-run]
set -euo pipefail

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=true
fi

REPO_NAME=""  # auto-detected from git remote
if [[ -z "$REPO_NAME" ]]; then
  REMOTE=$(git remote get-url origin 2>/dev/null || echo "")
  REPO_NAME=$(echo "$REMOTE" | sed -n 's|.*github.com[:/]\(.*\)\.git|\1|p' | sed 's|.*/||')
fi
[[ -z "$REPO_NAME" ]] && { echo "ERROR: cannot detect repo name"; exit 1; }

LINT_REPORT=$(mktemp)
trap 'rm -f "$LINT_REPORT"' EXIT

golangci-lint run ./... --out-format json > "$LINT_REPORT" 2>/dev/null || true

ERROR_COUNT=$(jq '.Issues | length' "$LINT_REPORT" 2>/dev/null || echo 0)
HEAD_SHA=$(git rev-parse HEAD 2>/dev/null || echo "unknown")

KNOWN_ISSUE_TITLE_PREFIX="lint(${REPO_NAME})"

close_existing_issue() {
  local search="$KNOWN_ISSUE_TITLE_PREFIX"
  local open_issue
  open_issue=$(gh issue list -R "X-didgital/$REPO_NAME" --state open --json number,title --jq ".[] | select(.title | startswith(\"$search\")) | .number" 2>/dev/null | head -1)
  if [[ -n "$open_issue" ]]; then
    if $DRY_RUN; then
      echo "[DRY-RUN] Would close #$open_issue"
    else
      gh issue close "$open_issue" -R "X-didgital/$REPO_NAME" --comment "All lint issues resolved." 2>/dev/null || true
    fi
  fi
}

if [[ "$ERROR_COUNT" -eq 0 ]]; then
  close_existing_issue
  exit 0
fi

TABLE=""
while IFS=$'\t' read -r file line linter text; do
  TABLE+="| \`$file\` | $line | $linter | $text |"$'\n'
done < <(jq -r '.Issues[] | [.Pos.Filename, (.Pos.Line|tostring), .FromLinter, .Text] | @tsv' "$LINT_REPORT" 2>/dev/null)

LINTER_SUMMARY=$(jq -r '.Issues | group_by(.FromLinter) | map("\(.[0].FromLinter): \(length)") | join(", ")' "$LINT_REPORT" 2>/dev/null)

BODY="## Репозиторий: $REPO_NAME
## SHA: $HEAD_SHA

Найдено **$ERROR_COUNT** ошибок линтинга.

### Сводка по линтерам

$LINTER_SUMMARY

### Ошибки

| Файл | Строка | Линтер | Ошибка |
|------|--------|--------|--------|
$TABLE

### Как исправить

1. Запустить локально: \`golangci-lint run ./...\`
2. Исправить ошибки
3. Закоммитить и запушить
4. Issue закроется автоматически после следующего CI прогона
"

ISSUE_TITLE="${KNOWN_ISSUE_TITLE_PREFIX}: ${ERROR_COUNT} violations (${LINTER_SUMMARY})"

EXISTING=$(gh issue list -R "X-didgital/$REPO_NAME" --state open --json number,title --jq ".[] | select(.title | startswith(\"$KNOWN_ISSUE_TITLE_PREFIX\")) | .number" 2>/dev/null | head -1)

if [[ -n "$EXISTING" ]]; then
  if $DRY_RUN; then
    echo "[DRY-RUN] Would update #$EXISTING"
  else
    gh issue edit "$EXISTING" -R "X-didgital/$REPO_NAME" --title "$ISSUE_TITLE" --body "$BODY" 2>/dev/null || true
  fi
else
  if $DRY_RUN; then
    echo "[DRY-RUN] Would create issue: $ISSUE_TITLE"
  else
    gh issue create -R "X-didgital/$REPO_NAME" --title "$ISSUE_TITLE" --body "$BODY" 2>/dev/null || true
  fi
fi
