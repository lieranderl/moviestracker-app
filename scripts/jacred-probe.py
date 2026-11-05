#!/usr/bin/env python3
"""Compare JacRed search-API terms for real titles (manual probe, not CI).

For each title it asks api.jacred.su with every candidate term the app
tries (IMDb id, English, original and Russian names), with the season for
series, and prints how many releases each finds, and how many of them pass
the app's year filter.

    JACRED_APIKEY=… TMDB_API_KEY=… python3 -I scripts/jacred-probe.py [movie:ID | tv:ID:SEASON …]
"""

import json
import os
import sys
import time
import unicodedata
import urllib.error
import urllib.parse
import urllib.request

JACRED, TMDB = os.environ["JACRED_APIKEY"], os.environ["TMDB_API_KEY"]
DEFAULT = [
    "movie:27205",
    "movie:496243",
    "movie:693134",
    "movie:1064213",
    "movie:157336",
    "tv:1396:5",
    "tv:136315:3",
    "tv:95396:2",
    "tv:126308:1",
    "tv:93405:2",
    "tv:2190:27",
    "tv:289517:1",
]


def get(url, headers=None):
    req = urllib.request.Request(
        url, headers={"Accept": "application/json", **(headers or {})}
    )
    with urllib.request.urlopen(req, timeout=40) as resp:
        return json.load(resp)


def tmdb(path, **params):
    headers = {}
    if len(TMDB) > 40:  # a v4 read token
        headers["Authorization"] = "Bearer " + TMDB
    else:
        params["api_key"] = TMDB
    return get(
        f"https://api.themoviedb.org/3/{path}?{urllib.parse.urlencode(params)}", headers
    )


def terms(kind, tmdb_id):
    d = tmdb(
        f"{kind}/{tmdb_id}",
        language="ru-RU",
        append_to_response="translations,external_ids",
    )
    en = next(
        (
            t["data"]
            for t in d["translations"]["translations"]
            if t["iso_639_1"] == "en"
        ),
        {},
    )
    original = d.get("original_title") or d.get("original_name")
    english = (
        en.get("title")
        or en.get("name")
        or (original if d.get("original_language") == "en" else "")
    )
    local = d.get("title") or d.get("name")
    year = int((d.get("release_date") or d.get("first_air_date") or "0")[:4] or 0)
    imdb = d.get("imdb_id") or d["external_ids"].get("imdb_id") or ""
    out = []
    for label, t in (
        ("imdb", imdb),
        ("english", english),
        ("original", original),
        ("russian", local),
    ):
        if t and t.lower() not in (x[1].lower() for x in out):
            out.append((label, t))
    return local, year, out


def norm(s):
    s = unicodedata.normalize("NFKD", s.lower()).replace("ё", "е").replace("ё", "е")
    s = "".join(c if c.isalnum() else " " for c in s if not unicodedata.combining(c))
    return " ".join(s.split())


def named(release, names):
    """The release's name, a "/" or "|" part of it, or the whole title (for a
    name with a slash, "Face/Off") starts with one of names."""
    title = release["title"]
    parts = [norm(p) for p in [*title.replace("|", "/").split("/"), title]]
    return any(p == n or p.startswith(n + " ") for p in parts for n in names)


def near_year(released, year):
    return year > 0 and year - 1 <= released <= year + 1


def belongs(release, year, season, season_year):
    """Query.belongs in internal/jacred/jacred.go."""
    released = release.get("year") or 0
    if season:
        if release.get("seasons") and season not in release["seasons"]:
            return False
        if released == 0:  # single-episode uploads; the season filter vouches
            return True
        return near_year(released, year) or near_year(released, season_year)
    if year == 0:
        return True
    if released == 0:
        return str(year) in release["title"]
    return near_year(released, year)


def search(term, season):
    params = {"query": term, "limit": 120}
    if season:
        params["season"] = season
    for _ in range(3):
        try:
            return get(
                "https://api.jacred.su/api/search?" + urllib.parse.urlencode(params),
                {"Authorization": "Bearer " + JACRED},
            )["results"]
        except urllib.error.HTTPError as e:
            wait = e.headers.get("Retry-After", "")
            if e.code != 429 or not wait.isdigit() or int(wait) > 300:
                return f"HTTP {e.code}" + (f" (retry after {wait}s)" if wait else "")
            print(f"           … 429, waiting {wait}s")
            time.sleep(int(wait) + 1)
    return "HTTP 429"


for spec in sys.argv[1:] or DEFAULT:
    kind, tmdb_id, *rest = spec.split(":")
    season = int(rest[0]) if rest else 0
    local, year, candidates = terms(kind, tmdb_id)
    print(
        f"\n{kind} {tmdb_id} {local} ({year})" + (f" season {season}" if season else "")
    )
    names = [norm(t) for label, t in candidates if label != "imdb"]
    season_year = 0
    if season:
        air = tmdb(f"tv/{tmdb_id}/season/{season}").get("air_date") or ""
        season_year = int(air[:4] or 0)
    got = {}
    for label, term in candidates:
        found = search(term, season)
        time.sleep(2)
        if isinstance(found, str):
            print(f"  {label:8} {term!r:40} {found}")
            continue
        near = [r for r in found if belongs(r, year, season, season_year)]
        got[label] = [r for r in near if named(r, names)]
        print(
            f"  {label:8} {term!r:40} {len(found):4d} found, {len(near):4d} kept,"
            f" {len(got[label]):4d} named after the title"
        )
        for r in [r for r in near if not named(r, names)][:1]:
            print(f"           ✗ {r['title'][:100]}")
    # What the app does: the first name, the IMDb id when that found under
    # 50, then the other names in turn until something is found.
    order = [label for label, _ in candidates if label != "imdb"]
    kept, asked = {}, []

    def add(label):
        asked.append(label)
        for r in got.get(label, []):
            kept.setdefault(r["magnet"].split("&")[0].lower(), r)

    if order:
        add(order[0])
    if any(label == "imdb" for label, _ in candidates) and len(kept) < 50:
        add("imdb")
    for label in order[1:]:
        if kept:
            break
        add(label)
    print(f"  → app asks {' + '.join(asked)}: {len(kept)} releases")
