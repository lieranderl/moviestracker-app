# shellcheck shell=bash
# Sourced by the release scripts: sets shared_tmdb_key, the TMDB key built
# into releases so search works before a user adds their own. It comes from
# MT_SHARED_TMDB_KEY or the git-ignored .tmdb-shared-key file, never from git.
# The sourcing script sets root, the repository folder.
: "${root:?}"
shared_tmdb_key="${MT_SHARED_TMDB_KEY:-}"
if [ -z "$shared_tmdb_key" ] && [ -f "$root/.tmdb-shared-key" ]; then
  shared_tmdb_key="$(tr -d '[:space:]' <"$root/.tmdb-shared-key")"
fi
if [ -z "$shared_tmdb_key" ]; then
  echo "note: no shared TMDB key (MT_SHARED_TMDB_KEY or .tmdb-shared-key): users must add their own" >&2
fi
