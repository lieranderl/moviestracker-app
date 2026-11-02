# Viewing audit fixes

Goal: repair the deployed localized-library search and season navigation gaps
before adding discovery presets. User authorized implementation and confirmed
signed-in HTTP, rendered HTML and browser keyboard test boundaries.

## Approach

- Preserve `tmdb.TitleProvider` through the web release-feed adapter. The
  wrapper currently embeds only `DetailsProvider`, so the catalog's optional
  localization capability disappears. Forward lightweight title lookups when
  available; retain saved-name fallback when unavailable or failing. Do not
  increase lookup deadlines or issue full movie/series requests as a fallback.
- Keep seasons as ordinary navigation links. Expose current season with
  `aria-current="page"`, updated from the existing Datastar season signal.
  Remove the incomplete tab role. Native Tab/Enter remains the keyboard model;
  a full tab widget would add unnecessary interaction machinery.
- Preserve shared components, English/Russian strings, CSP and existing
  app/web adapters. Production account data remains unchanged during checks.

Alternatives rejected: longer localization deadlines cannot repair a missing
interface; a new tab keyboard implementation is unnecessary for navigation links.
Search alias expansion is separate scope: this fix restores the localized name
the interface already promises, with existing fallback behavior.

## Acceptance

1. Signed-in web library finds an English-saved movie by its Russian name.
2. The same adapter localizes favourites, custom-list contents and recommendation
   seed headings; failed or unsupported naming preserves saved titles.
3. Rendered series navigation exposes the initial current season; browser
   Tab/Enter changes the episode list and current link without losing focus.
4. Existing tests, targeted race checks and full `make ci` pass; focused PRs
   follow the repository workflow. No personal state is changed in production.

Unresolved questions: none for these fixes. Library layout suggestions remain
separate from the two demonstrated defects.
