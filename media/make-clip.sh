#!/usr/bin/env bash
# Build 04-gate-clip.mp4 from the source screen recording.
#
# The source is one 4m14s capture of the whole session. Almost all of it is
# terminal output containing full profile GUIDs and quoted profile names, so it
# is not published. This script keeps the only two segments that carry the
# evidence and obscure every identifier in them:
#
#   1. 236.0-244.0s  the profile picker, the PIN prompt, the landing as the
#                    protected profile. One bar over the picker's name row and
#                    one over the signed-in profile menu.
#   2. 244.5-250.5s  the terminal block that produced the value: five rows of
#                    ProfileLockTemplate, one populated lockPin, four nulls.
#                    Bars over the GUID column, the quoted-name column, the
#                    operator's aside that repeats a name, and the two pg=
#                    values on the start/final lines. The crop is scaled 1.64x
#                    so 12px terminal text is readable in the player.
#
# The four-digit lockPin is deliberately left readable: it is the finding, and
# the lock it belonged to has since been removed from the account.
#
# Usage: ./make-video.sh "/path/to/2026-10-23_15-15-12.mp4"
set -euo pipefail

SRC="${1:?usage: make-video.sh /path/to/source.mp4}"
OUT="$(cd "$(dirname "$0")" && pwd)"

ffmpeg -y -v error -ss 236 -t 15 -i "$SRC" -filter_complex "\
[0:v]split=2[v1][v2];\
[v1]trim=start=0:end=8,setpts=PTS-STARTPTS,\
drawbox=x=880:y=645:w=820:h=56:color=0x111111:t=fill:enable='lt(t,4.65)',\
drawbox=x=2150:y=88:w=396:h=64:color=0x111111:t=fill:enable='gte(t,4.6)',\
scale=1280:540,pad=1280:720:0:90:black,setsar=1[a];\
[v2]trim=start=8.5:end=14.5,setpts=PTS-STARTPTS,\
drawbox=x=33:y=273:w=239:h=95:color=0x111111:t=fill,\
drawbox=x=279:y=273:w=72:h=95:color=0x111111:t=fill,\
drawbox=x=644:y=368:w=54:h=18:color=0x111111:t=fill,\
drawbox=x=76:y=481:w=278:h=22:color=0x111111:t=fill,\
drawbox=x=76:y=633:w=278:h=23:color=0x111111:t=fill,\
crop=700:440:0:240,scale=1145:720,pad=1280:720:67:0:white,setsar=1[b];\
[a][b]concat=n=2:v=1:a=0,format=yuv420p[out]" -map "[out]" -an \
  -c:v libx264 -crf 26 -preset slow -movflags +faststart "$OUT/04-gate-clip.mp4"

# Poster is the PIN gate, masked by the same filter chain.
ffmpeg -y -v error -ss 3 -i "$OUT/04-gate-clip.mp4" -frames:v 1 -q:v 3 "$OUT/03-gate-poster.jpg"

# Verification: no 30-char GUID and no profile name may survive in any frame,
# while the lockPin value and the RESULT line must still be readable.
tmp="$(mktemp -d)"
for i in $(seq 0 27); do
  ffmpeg -v error -ss "$(awk "BEGIN{print $i*0.5}")" -i "$OUT/04-gate-clip.mp4" \
    -frames:v 1 "$tmp/f$(printf %02d "$i").png" -y
done
bad=0
for f in "$tmp"/f*.png; do
  txt="$(tesseract "$f" stdout --psm 6 2>/dev/null || true)"
  if printf '%s' "$txt" | grep -Eq '[A-Z0-9]{28,32}' \
     || printf '%s' "$txt" | grep -Eqi 'shoo|sarmi|sandy|test2|addons'; then
    echo "LEAK in $f"; bad=1
  fi
done
last="$(ls "$tmp"/f*.png | tail -1)"
tesseract "$last" stdout --psm 6 2>/dev/null | grep -q "0978" || { echo "lockPin value missing"; bad=1; }
tesseract "$last" stdout --psm 6 2>/dev/null | grep -q "UNLOCKED" || { echo "RESULT line missing"; bad=1; }
rm -rf "$tmp"
[ "$bad" -eq 0 ] && echo "ok: no identifiers, evidence intact"
exit "$bad"
