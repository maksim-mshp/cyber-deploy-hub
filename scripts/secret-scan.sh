#!/bin/sh
set -eu

if command -v gitleaks >/dev/null 2>&1; then
  exec gitleaks detect --source . --config .gitleaks.toml --redact
fi

matches="$(
  {
    git grep -nI -E '(AKIA[0-9A-Z]{16}|-----BEGIN (RSA|OPENSSH|EC|DSA) PRIVATE KEY-----)' -- ':!.codex' ':!.info' ':!frontend/package-lock.json' ':!.env.example' || true
    git grep -nI -E '(password|secret|token|api[_-]?key)[[:space:]]*[:=][[:space:]]*["'\''][^"'\'']{16,}["'\'']' -- ':!.codex' ':!.info' ':!frontend/package-lock.json' ':!.env.example' || true
  } | sed '/^[[:space:]]*$/d'
)"

if [ -n "$matches" ]; then
  printf '%s\n' "$matches"
  printf '%s\n' "secret scan found suspicious tracked content"
  exit 1
fi

printf '%s\n' "secret scan passed"
