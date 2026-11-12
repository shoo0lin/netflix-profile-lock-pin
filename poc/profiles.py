"""Shared helpers for the profile-lock harnesses.

Nothing here is clever: parse a cookie file, find the profile GUID the session
is currently on (the `pg` parameter inside `NetflixId`), and POST a persisted
operation to the GraphQL endpoint. Standard library only.
"""
import json
import os
import re
import sys
import urllib.error
import urllib.request

GRAPHQL = "https://www.netflix.com/graphql"

# Persisted operation used by every script in this repository.
LOCK_TEMPLATE = ("ProfileLockTemplate", "d821963d-5bb5-43c7-a2d1-2cf7eab9f44e", 102)

UA = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36")

CHROME = os.environ.get("CHROME_BIN", "/opt/google/chrome/chrome")


def read_cookie_file(path):
    """Read the one-line `k=v; k=v` cookie string a browser exports."""
    try:
        return open(path).read().strip()
    except OSError as exc:
        sys.exit(f"cannot read cookie file {path!r}: {exc}")


def parse_cookies(cookie_string):
    """Cookie string -> Playwright/Chromium cookie dicts for www.netflix.com."""
    out = []
    for part in cookie_string.split("; "):
        if "=" not in part:
            continue
        name, value = part.split("=", 1)
        out.append({
            "name": name,
            "value": value,
            "domain": ".netflix.com",
            "path": "/",
            "secure": True,
            "httpOnly": name in ("NetflixId", "SecureNetflixId"),
            "sameSite": "None",
        })
    return out


def pg_of(source):
    """Current profile GUID from a cookie string or a list of cookie dicts."""
    if isinstance(source, list):
        value = next((c["value"] for c in source if c["name"] == "NetflixId"), "")
    else:
        m = re.search(r"(?:^|; )NetflixId=([^;]+)", source)
        value = m.group(1) if m else ""
    m = re.search(r"pg%3D([A-Z0-9]+)", value) or re.search(r"pg=([A-Z0-9]+)", value)
    return m.group(1) if m else None


def graphql(op_name, op_id, variables, version, cookie_string):
    """POST one persisted operation. Returns (status, headers, body text)."""
    body = json.dumps({
        "operationName": op_name,
        "variables": variables,
        "extensions": {"persistedQuery": {"id": op_id, "version": version}},
    }).encode()
    req = urllib.request.Request(GRAPHQL, data=body, method="POST", headers={
        "Content-Type": "application/json",
        "Accept": "*/*",
        "Origin": "https://www.netflix.com",
        "Referer": "https://www.netflix.com/",
        "User-Agent": UA,
        "Cookie": cookie_string,
    })
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status, dict(resp.headers), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as exc:
        return exc.code, dict(exc.headers), exc.read().decode("utf-8", "replace")


def pin(value):
    """`0978` -> `set (4 chars)`; the evidence is presence, not the digits."""
    return "null" if not value else f"set ({len(value)} chars)"


def require_own_account(enabled):
    """The one rule that matters: these scripts run against your own account."""
    if not enabled:
        sys.exit(
            "refusing to run without --confirm-own-account\n"
            "These harnesses act on the account whose cookies you supply. Use your\n"
            "own account, your own profiles, and put the PIN back when you are done."
        )
