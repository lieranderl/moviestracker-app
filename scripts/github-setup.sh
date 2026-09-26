#!/usr/bin/env bash
# Applies the repository's GitHub settings with the gh CLI. Safe to run again:
# it updates what exists. Needs a token with the repo scope (gh auth login),
# and a public repository (or a paid plan) for rulesets and environments.
#
#   scripts/github-setup.sh [owner/repo]    (default: lieranderl/moviestracker-app)
#
# - merges: squash only, PR title as the commit, branches deleted after merge
# - labels used by the release notes and Dependabot
# - Dependabot alerts and security updates, secret scanning with push
#   protection, private vulnerability reporting
# - Actions: read-only token by default, workflows from outside contributors
#   wait for approval
# - rulesets: main changes only through pull requests with CI green; only
#   admins create v* tags, and nobody moves or deletes them
# - immutable releases: a published release's files and tag stay as they are
# - the `release` environment, which only v* tags can use, with the
#   MT_SHARED_TMDB_KEY secret from .tmdb-shared-key when present
#   JACRED_APIKEY secret (jacred.su project key) from .jacred-shared-key when present
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

say "Dependabot alerts and security updates, secret scanning, vulnerability reports"
gh api -X PUT "repos/$repo/vulnerability-alerts" --silent
gh api -X PUT "repos/$repo/automated-security-fixes" --silent
gh api -X PUT "repos/$repo/private-vulnerability-reporting" --silent
gh api -X PATCH "repos/$repo" --input - --silent <<<'{"security_and_analysis": {
  "secret_scanning": {"status": "enabled"},
  "secret_scanning_push_protection": {"status": "enabled"}
}}'

say "Actions: read-only token, outside contributors' workflows wait for approval"
gh api -X PUT "repos/$repo/actions/permissions/workflow" --silent \
  -f default_workflow_permissions=read -F can_approve_pull_request_reviews=false
gh api -X PUT "repos/$repo/actions/permissions/fork-pr-contributor-approval" --silent \
  -f approval_policy=all_external_contributors

# ruleset <name> <json>: creates the ruleset, or updates the one with that name.
ruleset() {
  local id
  id="$(gh api "repos/$repo/rulesets" --jq ".[] | select(.name == \"$1\") | .id")"
  if [ -n "$id" ]; then
    gh api -X PUT "repos/$repo/rulesets/$id" --input - --silent <<<"$2"
  else
    gh api -X POST "repos/$repo/rulesets" --input - --silent <<<"$2"
  fi
}

# GitHub Actions' app id, for required checks that must come from our workflows.
actions=15368
check() { printf '{"context":"%s","integration_id":%d}' "$1" "$actions"; }

# Nobody bypasses it, admins included: every change is a pull request with CI
# green, squash-merged, so GitHub makes and signs every commit on main (a
# required_signatures rule would also demand signed commits on every branch).
# No approval is required while there is one maintainer: raise
# required_approving_review_count (and turn on require_code_owner_review) when
# there are more.
say "Ruleset: main"
ruleset main "$(
  cat <<JSON
{
  "name": "main",
  "target": "branch",
  "enforcement": "active",
  "conditions": {"ref_name": {"include": ["~DEFAULT_BRANCH"], "exclude": []}},
  "bypass_actors": [],
  "rules": [
    {"type": "deletion"},
    {"type": "non_fast_forward"},
    {"type": "required_linear_history"},
    {"type": "pull_request", "parameters": {
      "required_approving_review_count": 0,
      "dismiss_stale_reviews_on_push": true,
      "require_code_owner_review": false,
      "require_last_push_approval": false,
      "required_review_thread_resolution": true,
      "allowed_merge_methods": ["squash"]
    }},
    {"type": "required_status_checks", "parameters": {
      "strict_required_status_checks_policy": true,
      "do_not_enforce_on_create": false,
      "required_status_checks": [
        $(check "CI passed"),
        $(check "Title follows Conventional Commits"),
        $(check "Dependency review")
      ]
    }},
    {"type": "code_scanning", "parameters": {"code_scanning_tools": [
      {"tool": "CodeQL", "alerts_threshold": "errors", "security_alerts_threshold": "high_or_higher"}
    ]}}
  ]
}
JSON
)"

say "Rulesets: release tags"
ruleset "release tags: admins create" '{
  "name": "release tags: admins create",
  "target": "tag",
  "enforcement": "active",
  "conditions": {"ref_name": {"include": ["refs/tags/v*"], "exclude": []}},
  "bypass_actors": [{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}],
  "rules": [{"type": "creation"}]
}'
ruleset "release tags: immutable" '{
  "name": "release tags: immutable",
  "target": "tag",
  "enforcement": "active",
  "conditions": {"ref_name": {"include": ["refs/tags/v*"], "exclude": []}},
  "bypass_actors": [],
  "rules": [{"type": "update"}, {"type": "deletion"}, {"type": "non_fast_forward"}]
}'

say "Immutable releases"
gh api -X PUT "repos/$repo/immutable-releases" --silent

say "Environment: release, for v* tags only"
gh api -X PUT "repos/$repo/environments/release" --input - --silent <<<'{
  "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}
}'
if ! gh api "repos/$repo/environments/release/deployment-branch-policies" \
  --jq '.branch_policies[] | select(.type == "tag" and .name == "v*") | .id' | grep -q .; then
  gh api -X POST "repos/$repo/environments/release/deployment-branch-policies" --silent \
    -f name='v*' -f type=tag
fi

if [ -f "$root/.tmdb-shared-key" ]; then
  say "Secret: MT_SHARED_TMDB_KEY in the release environment (from .tmdb-shared-key)"
  tr -d '[:space:]' <"$root/.tmdb-shared-key" | gh secret set MT_SHARED_TMDB_KEY --env release --repo "$repo"
else
  say "No .tmdb-shared-key: set MT_SHARED_TMDB_KEY yourself for releases to carry the shared key:"
  say "  gh secret set MT_SHARED_TMDB_KEY --env release --repo $repo"
fi
# A repository-wide copy would reach every branch's workflows.
if gh secret list --repo "$repo" --json name --jq '.[].name' | grep -qx MT_SHARED_TMDB_KEY; then
  say "  removing the repository-wide MT_SHARED_TMDB_KEY"
  gh secret delete MT_SHARED_TMDB_KEY --repo "$repo"
fi

if [ -f "$root/.jacred-shared-key" ]; then
  say "Secret: JACRED_APIKEY in the release environment (from .jacred-shared-key)"
  tr -d '[:space:]' <"$root/.jacred-shared-key" | gh secret set JACRED_APIKEY --env release --repo "$repo"
else
  say "No .jacred-shared-key: set JACRED_APIKEY (Moviestracker's jacred.su project key) yourself for releases to carry it:"
  say "  gh secret set JACRED_APIKEY --env release --repo $repo"
fi
if gh secret list --repo "$repo" --json name --jq '.[].name' | grep -qx JACRED_APIKEY; then
  say "  removing the repository-wide JACRED_APIKEY"
  gh secret delete JACRED_APIKEY --repo "$repo"
fi

say "Done: https://github.com/$repo/settings"
