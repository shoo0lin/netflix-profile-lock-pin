#!/usr/bin/env python3
"""Negative controls: prove the lock is real and the PIN is the reason it opened.

    --mode no-pin   select the locked tile, submit nothing   -> expect BLOCKED
    --mode wrong    select the locked tile, submit 0000      -> expect BLOCKED
    --mode right    select the locked tile, submit the PIN   -> expect UNLOCKED

Run all three with the same cookie file and the same target GUID. `right` is the
one that must differ; if `wrong` also switches profile, the lock is decorative
and the finding is bigger -- or the harness is wrong. Check in that order.

    python3 poc/controls.py --cookies cookies-kids.txt \
        --target <protected-guid> --mode wrong --confirm-own-account
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
            f"{args.profile_dir}-{args.mode}", executable_path=profiles.CHROME,
            headless=not args.headed,
            args=["--no-sandbox", "--disable-blink-features=AutomationControlled"])
        page = ctx.pages[0] if ctx.pages else await ctx.new_page()
        await ctx.add_cookies(profiles.parse_cookies(profiles.read_cookie_file(args.cookies)))

        start = profiles.pg_of(await ctx.cookies())
        print(f"[{args.mode}] start pg={start}")
        await page.goto("https://www.netflix.com/ProfilesGate",
                        wait_until="domcontentloaded", timeout=60000)
        await page.wait_for_timeout(3000)
        await page.locator(f'[data-uia="profile-selector+tile-{args.target}"]').first.click(timeout=8000)
        await page.wait_for_timeout(4000)

        prompt = any(await page.locator(s).count() for s in PROMPT_FIELDS)
        print(f"[{args.mode}] PIN prompt visible: {prompt}")

        if args.mode in ("wrong", "right"):
            field = next((s for s in PROMPT_FIELDS if await page.locator(s).count()), None)
            if field:
                await page.locator(field).first.fill(args.pin)
                for sel in SUBMIT:
                    if await page.locator(sel).count():
                        try:
                            await page.locator(sel).first.click(timeout=5000)
                            break
                        except Exception:
                            pass
            await page.wait_for_timeout(5000)

        final = profiles.pg_of(await ctx.cookies())
        print(f"[{args.mode}] final pg={final} url={page.url}")
        switched = final == args.target
        print(f"[{args.mode}] RESULT: " + ("SWITCHED(!!)" if switched else "BLOCKED"))
        await ctx.close()
        expected = args.mode != "right"
        return 0 if switched == expected else 1


if __name__ == "__main__":
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--cookies", required=True)
    ap.add_argument("--target", required=True)
    ap.add_argument("--mode", required=True, choices=("no-pin", "wrong", "right"))
    ap.add_argument("--pin", default="0000", help="PIN to submit (mode wrong/right)")
    ap.add_argument("--profile-dir", default="/tmp/netflix-lockpin-controls")
    ap.add_argument("--headed", action="store_true")
    ap.add_argument("--confirm-own-account", action="store_true")
    parsed = ap.parse_args()
    profiles.require_own_account(parsed.confirm_own_account)
    sys.exit(asyncio.run(main(parsed)))
