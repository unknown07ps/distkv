#!/bin/bash
# ─────────────────────────────────────────────
#  DistKV — Git Push Script
# ─────────────────────────────────────────────

set -e  # exit on any error

# ── Config ────────────────────────────────────
BRANCH="${1:-main}"
COMMIT_MSG="${2:-update: $(date '+%Y-%m-%d %H:%M:%S')}"

# ── Colours ───────────────────────────────────
GREEN="\033[0;32m"
BLUE="\033[0;34m"
RED="\033[0;31m"
RESET="\033[0m"

echo -e "${BLUE}▶ DistKV Git Push${RESET}"
echo "  Branch  : $BRANCH"
echo "  Message : $COMMIT_MSG"
echo ""

# ── Safety checks ─────────────────────────────
if [ ! -d ".git" ]; then
  echo -e "${RED}✗ Not a git repository. Run: git init${RESET}"
  exit 1
fi

# ── Stage all changes ─────────────────────────
echo -e "${BLUE}[1/4] Staging changes...${RESET}"
git add .
git status --short

# ── Commit ────────────────────────────────────
echo -e "\n${BLUE}[2/4] Committing...${RESET}"
if git diff --cached --quiet; then
  echo "  Nothing to commit, working tree clean."
else
  git commit -m "$COMMIT_MSG"
  echo -e "${GREEN}  ✓ Committed${RESET}"
fi

# ── Ensure branch exists ──────────────────────
echo -e "\n${BLUE}[3/4] Checking branch '$BRANCH'...${RESET}"
if ! git show-ref --verify --quiet "refs/heads/$BRANCH"; then
  git checkout -b "$BRANCH"
  echo -e "${GREEN}  ✓ Created branch '$BRANCH'${RESET}"
else
  git checkout "$BRANCH" 2>/dev/null || true
fi

# ── Push ──────────────────────────────────────
echo -e "\n${BLUE}[4/4] Pushing to origin/$BRANCH...${RESET}"
git push -u origin "$BRANCH"
echo -e "\n${GREEN}✓ Successfully pushed to origin/$BRANCH${RESET}"
