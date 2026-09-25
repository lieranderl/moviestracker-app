# Security policy

## Reporting a problem

Please report security problems **privately**, not in a public issue:
[open a security advisory](https://github.com/lieranderl/moviestracker-app/security/advisories/new)
on GitHub. Include what an attacker could do, the steps to reproduce it and
the version you tested (it is in the page footer).

You will get an answer within a week. Once a fix is released, the advisory is
published with credit to you, unless you prefer otherwise.

## Supported versions

Only the latest release gets security fixes. The Mac app and the Linux
installer upgrade in place, so updating is always the fix.

## What is in scope

Moviestracker is a self-hosted app for a home network. Problems like these
are in scope:

- getting in without an account, or doing an administrator's work as a viewer;
- reading or changing another person's data, keys or settings;
- making someone's browser act on Moviestracker from another site (CSRF,
  DNS rebinding, XSS, including through titles or file names from TMDB,
  JacRed or torrents);
- reaching TorrServer or other machines through Moviestracker in ways the
  app does not offer;
- stream links that reach more than the one file they were made for;
- secrets that end up in logs, pages, releases or the repository.

Out of scope: denial of service by someone already signed in, the plain-HTTP
default on a trusted home network (put an HTTPS reverse proxy in front and set
`MT_SECURE_COOKIES=true` when exposing it), the shared TMDB key being readable
in release binaries (it is a read-only key made for this), and problems in
TorrServer, GStreamer or JacRed themselves (please report those upstream).

## How Moviestracker protects you

- Every page needs a local account; passwords are bcrypt hashes and only
  hashes of session tokens are stored.
- The first administrator can only be created from the machine itself, or
  with the one-time setup code the server prints when it starts.
- Cross-site requests are refused, and requests for unknown host names are
  refused to stop DNS rebinding.
- A managed TorrServer listens only on `127.0.0.1` behind a generated
  password; pages and playlists never contain its address.
- Links for external players are signed, expire after 7 days, reach only one
  file, and can all be cancelled at once in Settings → Security.
- Keys are never sent to the browser; the settings file is readable only by
  the account that runs Moviestracker.
- Every change is checked by `govulncheck`, `gosec`, CodeQL and a secret scan
  of the whole history.
