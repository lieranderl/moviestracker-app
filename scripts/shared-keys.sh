# shellcheck shell=bash
# Sourced by the release scripts: sets the keys built into releases, from the
# environment or git-ignored files, never from git.
#   shared_tmdb_key    TMDB key, so search works before a user adds their own
#                      (MT_SHARED_TMDB_KEY or .tmdb-shared-key)
#   shared_jacred_key  Moviestracker's jacred.su project key, unlimited, for
#                      jacred.su only (MT_SHARED_JACRED_KEY or .jacred-shared-key)
# The sourcing script sets root, the repository folder.
: "${root:?}"
shared_tmdb_key="${MT_SHARED_TMDB_KEY:-}"
if [ -z "$shared_tmdb_key" ] && [ -f "$root/.tmdb-shared-key" ]; then
  shared_tmdb_key="$(tr -d '[:space:]' <"$root/.tmdb-shared-key")"
fi
if [ -z "$shared_tmdb_key" ]; then
  echo "note: no shared TMDB key (MT_SHARED_TMDB_KEY or .tmdb-shared-key): users must add their own" >&2
fi
shared_jacred_key="${MT_SHARED_JACRED_KEY:-}"
if [ -z "$shared_jacred_key" ] && [ -f "$root/.jacred-shared-key" ]; then
  shared_jacred_key="$(tr -d '[:space:]' <"$root/.jacred-shared-key")"
fi
if [ -z "$shared_jacred_key" ]; then
  echo "note: no shared JacRed key (MT_SHARED_JACRED_KEY or .jacred-shared-key): users must add their own" >&2
fi
