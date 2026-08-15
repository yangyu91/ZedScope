#!/usr/bin/env bash
# =============================================================================
# Push ZedScope to a NEW GitHub repository, then trigger the APK build.
#
# Prerequisites (run on a machine WITH internet access):
#   - `gh` CLI installed and on PATH
#   - a GitHub Personal Access Token with `repo` scope, exported as GH_TOKEN
#
# Usage:
#   export GH_TOKEN=ghp_xxxxxxxxxxxx
#   ./push.sh
#
# SECURITY: the token is read from the GH_TOKEN env var ONLY and is never
# written into the repository. Rotate / delete the token after the push.
# =============================================================================
set -euo pipefail

REPO="ZedScope"
DESC="ZedScope · 全端互通的安全免费 AI 浏览器：内置 MITM 抓包 + 改包 + Token 提取 + 本地 AI 中转站 + DeepSeek 白嫖桥 + 无权限浏览器 Agent。参考 proxypin / youtoo / sukisu-ui / cc-switch / deepseek-pp / Coomi-Android。"

# ---- preflight checks ----
if ! command -v gh >/dev/null 2>&1; then
  echo "ERROR: 'gh' CLI not found. Install it: https://cli.github.com/" >&2
  exit 1
fi
if [ -z "${GH_TOKEN:-}" ]; then
  echo "ERROR: set GH_TOKEN first:  export GH_TOKEN=ghp_xxx" >&2
  exit 1
fi

# Authenticate only if not already logged in (avoids re-login failures).
if ! gh auth status >/dev/null 2>&1; then
  echo "$GH_TOKEN" | gh auth login --with-token
fi
USER=$(gh api user --jq .login)
echo "Authenticated as: $USER"

REMOTE="https://$GH_TOKEN@github.com/$USER/$REPO.git"

# ---- initialize git if needed ----
if [ ! -d .git ]; then
  git init -q
  git checkout -q -b main 2>/dev/null || git branch -M main
fi

# ---- create the remote repo (idempotent) ----
if gh repo view "$USER/$REPO" >/dev/null 2>&1; then
  echo "Repository $USER/$REPO already exists — reusing it."
else
  gh repo create "$REPO" --public --description "$DESC" \
    --source . --push 2>/dev/null \
    && echo "Created + pushed $REPO via gh repo create --push." \
    && exit 0 || echo "(gh repo create --push failed; falling back to manual push)"
fi

# ---- manual push fallback ----
git add -A
if git diff --cached --quiet; then
  echo "(nothing new to commit)"
else
  git commit -q -m "Initial commit: ZedScope AI capture browser (Go core + Android shell)"
fi

git branch -M main
git remote remove origin 2>/dev/null || true
git remote add origin "$REMOTE"
git push -u origin main --force-with-lease

echo
echo "=========================================================="
echo " Pushed to https://github.com/$USER/$REPO"
echo
echo " Next: open Actions to get the APK"
echo "   https://github.com/$USER/$REPO/actions"
echo "   1) find the 'Build APK' run (triggered automatically)"
echo "   2) wait for it to finish"
echo "   3) download the 'ZedScope-apk' artifact -> install ZedScope.apk"
echo "=========================================================="
