#!/usr/bin/env bash
# Applies the repository's GitHub settings with the gh CLI. Safe to run again:
# it updates what exists. Needs a token with the repo scope (gh auth login).
#
#   scripts/github-setup.sh [owner/repo]    (default: lieranderl/moviestracker-app)
#
# - merges: squash only, PR title as the commit, branches deleted after merge
# - labels used by the release notes and Dependabot
# - Dependabot alerts and security updates; private vulnerability reporting
# - rulesets: main only through pull requests that pass CI; release tags
#   cannot be moved or deleted (GitHub enforces rulesets on public
#   repositories, or private ones on a paid plan)
# - the MT_SHARED_TMDB_KEY secret, from .tmdb-shared-key when present
set -euo pipefail

repo="${1:-lieranderl/moviestracker-app}"
root="$(cd "$(dirname "$0")/.." && pwd)"
say() { printf '%s\n' "$*"; }

say "Merge settings"
gh api -X PATCH "repos/$repo" --silent \
  -F allow_squash_merge=true -F allow_merge_commit=false -F allow_rebase_merge=false \
  -f squash_merge_commit_title=PR_TITLE -f squash_merge_commit_message=PR_BODY \
  -F delete_branch_on_merge=true -F allow_auto_merge=true -F allow_update_branch=true \
  -F has_wiki=false -F has_projects=false -F has_issues=true

say "Labels"
label() { # <name> <color> <description>
  gh label create "$1" --repo "$repo" --color "$2" --description "$3" --force >/dev/null
}
label bug d73a4a "Something does not work"
label feature 0e8a16 "New feature or improvement"
label fix 1d76db "Bug fix"
label breaking-change b60205 "Changes behavior people rely on"
label dependencies 0366d6 "Dependency update"
label go 00add8 "Go modules"
label frontend f9d0c4 "CSS and JavaScript tools"
label ci 5319e7 "Workflows and GitHub Actions"
label docker 2496ed "Container image"
label skip-changelog cccccc "Leave out of the release notes"

say "Dependabot alerts and security updates"
gh api -X PUT "repos/$repo/vulnerability-alerts" --silent
gh api -X PUT "repos/$repo/automated-security-fixes" --silent
gh api -X PUT "repos/$repo/private-vulnerability-reporting" --silent 2>/dev/null ||
  say "  private vulnerability reporting: available once the repository is public"

# ruleset <name> <json>: creates the ruleset, or updates the one with that name.
ruleset() {
  local id
  id="$(gh api "repos/$repo/rulesets" --jq ".[] | select(.name == \"$1\") | .id" 2>/dev/null || true)"
  if [ -n "$id" ]; then
    gh api -X PUT "repos/$repo/rulesets/$id" --input - --silent <<<"$2"
  else
    gh api -X POST "repos/$repo/rulesets" --input - --silent <<<"$2"
  fi
}

# GitHub Actions' app id, for required checks that must come from our workflows.
actions=15368
check() { printf '{"context":"%s","integration_id":%d}' "$1" "$actions"; }

say "Ruleset: main"
main_rules=$(
  cat <<JSON
{
  "name": "main",
  "target": "branch",
  "enforcement": "active",
  "conditions": {"ref_name": {"include": ["~DEFAULT_BRANCH"], "exclude": []}},
  "bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "pull_request"}],
  "rules": [
    {"type": "deletion"},
    {"type": "non_fast_forward"},
    {"type": "required_linear_history"},
    {"type": "pull_request", "parameters": {
      "required_approving_review_count": 1,
      "dismiss_stale_reviews_on_push": true,
      "require_code_owner_review": true,
      "require_last_push_approval": false,
      "required_review_thread_resolution": true,
      "allowed_merge_methods": ["squash"]
    }},
    {"type": "required_status_checks", "parameters": {
      "strict_required_status_checks_policy": true,
      "required_status_checks": [
        $(check "Lint, test, security (Linux)"),
        $(check "Secret scan (whole history)"),
        $(check "Tests and Mac app (macOS)"),
        $(check "Linux install, upgrade, uninstall (systemd)"),
        $(check "Windows tests, installer, install and uninstall"),
        $(check "Container image"),
        $(check "Title follows Conventional Commits")
      ]
    }}
  ]
}
JSON
)
say "Ruleset: release tags"
tag_rules='{
  "name": "release tags",
  "target": "tag",
  "enforcement": "active",
  "conditions": {"ref_name": {"include": ["refs/tags/v*"], "exclude": []}},
  "bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}],
  "rules": [{"type": "deletion"}, {"type": "non_fast_forward"}, {"type": "update"}]
}'
if ! ruleset main "$main_rules" || ! ruleset "release tags" "$tag_rules"; then
  say "  Rulesets were not applied: GitHub enforces them on public repositories,"
  say "  or on private ones with GitHub Pro. Run this again after making it public."
fi

if [ -f "$root/.tmdb-shared-key" ]; then
  say "Secret: MT_SHARED_TMDB_KEY (from .tmdb-shared-key)"
  tr -d '[:space:]' <"$root/.tmdb-shared-key" | gh secret set MT_SHARED_TMDB_KEY --repo "$repo"
else
  say "No .tmdb-shared-key: set MT_SHARED_TMDB_KEY yourself for releases to carry the shared key:"
  say "  gh secret set MT_SHARED_TMDB_KEY --repo $repo"
fi

say "Done: https://github.com/$repo/settings"
