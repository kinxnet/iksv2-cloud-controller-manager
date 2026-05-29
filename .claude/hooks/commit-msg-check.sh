#!/usr/bin/env bash
# Validates commit message subject against Conventional Commits.
# Fails open on anything it can't parse.

set -euo pipefail

# Fail open if jq is unavailable or stdin is unparseable.
cmd=$(jq -r '.tool_input.command // ""') || exit 0

# Only run on `git commit` (same matcher as the protected-branch hook).
if ! printf '%s' "$cmd" | grep -qE '(^|[[:space:];&|(])git[[:space:]]+([^;&|[:space:]]+[[:space:]]+)*commit([[:space:]]|$)'; then
  exit 0
fi

# Skip special forms where subject validation is not meaningful.
if printf '%s' "$cmd" | grep -qE -- '(--amend|--fixup|--squash|-C[[:space:]=]|--reuse-message)'; then
  exit 0
fi

subject=$(CMD="$cmd" python3 - <<'PY'
import os, re, sys

cmd = os.environ.get("CMD", "")

# Heredoc form: git commit -m "$(cat <<'EOF'\n<subject>\n...\nEOF\n)"
m = re.search(r"<<-?\s*['\"]?([A-Za-z_][A-Za-z0-9_]*)['\"]?\s*\n(.*?)\n\1\b", cmd, re.DOTALL)
if m:
    for line in m.group(2).splitlines():
        s = line.strip()
        if s:
            print(s)
            sys.exit(0)

# Plain -m "subject" / -m 'subject' form.
m = re.search(r"-m\s+(['\"])((?:\\.|(?!\1).)*)\1", cmd, re.DOTALL)
if m:
    first = next((l.strip() for l in m.group(2).splitlines() if l.strip()), "")
    if first:
        print(first)
PY
)

if [ -z "$subject" ]; then
  exit 0
fi

pattern='^(feat|fix|docs|style|refactor|test|chore|perf|ci|build|revert)(\([^)]+\))?!?:[[:space:]].+'
if printf '%s' "$subject" | grep -qE "$pattern"; then
  exit 0
fi

jq -n --arg s "$subject" '{
  hookSpecificOutput: {
    hookEventName: "PreToolUse",
    permissionDecision: "deny",
    permissionDecisionReason: (
      "commit message does not follow Conventional Commits:\n  \($s)\n\n" +
      "expected: <type>(<scope>)?: <description>\n" +
      "allowed types: feat, fix, docs, style, refactor, test, chore, perf, ci, build, revert\n" +
      "examples: \"fix: prevent auth leak\", \"refactor(api): simplify handler\""
    )
  }
}'
