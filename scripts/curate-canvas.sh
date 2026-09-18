#!/usr/bin/env bash
# curate-canvas.sh — cut a looping, portrait, silent background-video clip
# (Spotify-Canvas-style) from a full source video, and drop it straight into
# maraetai-service's served videos directory.
#
# The server looks up clips purely by filename (<VIDEOS_DIR>/<id>.mp4, where
# id is a Subsonic song or album id) — this script IS the curation step; there
# is no upload endpoint. Run it by hand, once per track/album you curate.
#
# Usage:
#   scripts/curate-canvas.sh -i SOURCE -s START -d DURATION -o ID [options]
#
# Required:
#   -i SOURCE     Path to the full source video.
#   -s START      Start time in the source, as ffmpeg accepts it (e.g. 12.5 or 00:00:12.5).
#   -d DURATION   Clip length in seconds (e.g. 8). Spotify Canvas uses 3-8s;
#                 for a personal library, longer (8-15s) is often better —
#                 it makes the loop seam far less noticeable.
#   -o ID         Target Subsonic ID (song id for a per-track clip, album id
#                 for a per-album clip) — becomes the output filename.
#
# Options:
#   -x SECONDS    Crossfade the clip's tail into its head to smooth the loop
#                 point (default: 0.4s). Set to 0 to disable and just trim.
#   -w WIDTH      Output width  (default: 1080).
#   -h HEIGHT     Output height (default: 1920 — portrait 9:16, phone-only).
#   -O OUTPUT_DIR Directory to write <ID>.mp4 into (default: ./data/videos,
#                 matching the service's local-dev default; pass /data/videos
#                 to curate directly against a running container's volume).
#
# The id is validated against the exact same character set the server's
# getTrackVideo endpoint accepts (^[A-Za-z0-9_-]{1,128}$) — a file this script
# would refuse to name, the server would refuse to serve anyway.

set -euo pipefail

XFADE=0.4
WIDTH=1080
HEIGHT=1920
OUTPUT_DIR="./data/videos"

usage() {
  grep '^#' "$0" | sed -e 's/^#!.*//' -e 's/^# \{0,1\}//'
  exit 1
}

while getopts "i:s:d:o:x:w:h:O:" opt; do
  case "$opt" in
    i) SOURCE="$OPTARG" ;;
    s) START="$OPTARG" ;;
    d) DURATION="$OPTARG" ;;
    o) ID="$OPTARG" ;;
    x) XFADE="$OPTARG" ;;
    w) WIDTH="$OPTARG" ;;
    h) HEIGHT="$OPTARG" ;;
    O) OUTPUT_DIR="$OPTARG" ;;
    *) usage ;;
  esac
done

: "${SOURCE:?-i SOURCE is required}"
: "${START:?-s START is required}"
: "${DURATION:?-d DURATION is required}"
: "${ID:?-o ID is required}"

if [[ ! "$ID" =~ ^[A-Za-z0-9_-]{1,128}$ ]]; then
  echo "error: id '$ID' doesn't match ^[A-Za-z0-9_-]{1,128}\$ — the server would reject this filename too" >&2
  exit 1
fi

# DURATION and XFADE feed into awk expressions below (BEGIN{...} program
# text) — unlike ID, these were previously interpolated unvalidated, which is
# a command-injection primitive (a crafted -d value can break out of the
# arithmetic and call awk's system()). Restrict to plain decimal numbers
# before they ever reach awk, and pass them as -v variables (never spliced
# into program source) as a second, independent line of defense.
if [[ ! "$DURATION" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
  echo "error: -d duration '$DURATION' must be a plain number (e.g. 8 or 7.5)" >&2
  exit 1
fi
if [[ ! "$XFADE" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
  echo "error: -x crossfade '$XFADE' must be a plain number (e.g. 0.4 or 0)" >&2
  exit 1
fi
if [[ ! "$WIDTH" =~ ^[0-9]+$ || ! "$HEIGHT" =~ ^[0-9]+$ ]]; then
  echo "error: -w/-h must be plain integers" >&2
  exit 1
fi

if [[ ! -f "$SOURCE" ]]; then
  echo "error: source video not found: $SOURCE" >&2
  exit 1
fi

command -v ffmpeg >/dev/null || { echo "error: ffmpeg not found on PATH" >&2; exit 1; }

mkdir -p "$OUTPUT_DIR"
OUT="$OUTPUT_DIR/$ID.mp4"
TMP_TRIMMED="$(mktemp --suffix=.mp4)"
trap 'rm -f "$TMP_TRIMMED"' EXIT

# Center-crop-to-fill WIDTHxHEIGHT (portrait), then trim to the requested
# window. -an drops audio entirely — the client already plays the real track
# audio; a second audio stream would fight it.
CROP_FILTER="scale=w='if(gt(a,${WIDTH}/${HEIGHT}),-2,${WIDTH})':h='if(gt(a,${WIDTH}/${HEIGHT}),${HEIGHT},-2)',crop=${WIDTH}:${HEIGHT}"

echo "==> Trimming & cropping to ${WIDTH}x${HEIGHT}, ${DURATION}s from ${START}..."
ffmpeg -y -loglevel error -ss "$START" -i "$SOURCE" -t "$DURATION" \
  -vf "$CROP_FILTER" -an -c:v libx264 -preset medium -crf 18 \
  "$TMP_TRIMMED"

if [[ "$XFADE" == "0" ]]; then
  echo "==> No crossfade requested — using the trimmed clip as-is."
  mv "$TMP_TRIMMED" "$OUT"
else
  # ffmpeg's trim filter takes literal time values, not expressions — compute
  # the tail-start / mid-end point (duration - crossfade) up front. Values are
  # passed via -v, never interpolated into the awk program text, even though
  # both are now validated as plain numbers above — belt and suspenders.
  REM="$(awk -v d="$DURATION" -v x="$XFADE" 'BEGIN { printf "%.3f", d - x }')"
  if awk -v rem="$REM" -v x="$XFADE" 'BEGIN { exit !(rem <= x) }'; then
    echo "error: -x crossfade (${XFADE}s) must be less than half of -d duration (${DURATION}s)" >&2
    exit 1
  fi

  # xfade requires its two inputs to carry an explicit constant frame rate on
  # the filter link — trim+setpts alone doesn't guarantee that (real-world
  # sources, e.g. an AV1 music video, can leave it unset even though the
  # source itself is CFR), which fails with "needs to be a constant frame
  # rate; current rate of 1/0 is invalid". Stamp it explicitly with the
  # trimmed clip's own rate rather than hardcoding one.
  FPS="$(ffprobe -v error -select_streams v:0 -show_entries stream=r_frame_rate -of csv=p=0 "$TMP_TRIMMED")"

  # Seamless-loop crossfade: blend the clip's own tail into its own head
  # (duration XFADE), then concat that blended join with the untouched
  # middle section. The result loops with no visible cut at the seam.
  #
  # Three things the concat/xfade graph needs pinned explicitly, each found
  # by a real iOS device silently failing to play a clip that downloaded and
  # ffprobed fine (AVFoundation is far stricter about all three than ffmpeg's
  # own tolerant demuxer):
  #  - fps=${FPS} on the final output: concat's blended segment reports a
  #    ragged average frame rate (e.g. 6000/239) that confuses libx264's
  #    level/macroblock-rate accounting into a bogus warning, and leaves the
  #    stream not clearly CFR. Restamping to the source rate keeps it clean.
  #  - format=yuv420p: xfade blends internally in a higher-precision pixel
  #    format (yuv444p), and without an explicit convert-back, libx264
  #    inherits that and encodes "High 4:4:4 Predictive" instead of 4:2:0 —
  #    iOS's hardware decoder (VideoToolbox) only supports 4:2:0 and silently
  #    fails to decode the video track. The file still downloads and probes
  #    fine, since the container itself is well-formed — the decode failure
  #    is invisible to anything but an actual hardware-decode attempt.
  # -avoid_negative_ts make_zero / -movflags +faststart: NOT a fix for
  # anything broken (a first DTS slightly behind PTS=0 is normal B-frame
  # reorder delay, present in any encode with B-frames, xfade or not — a
  # working plain trim/crop encode shows the exact same pattern) — kept as
  # standard streaming hygiene: faststart puts moov before mdat so a client
  # can start decoding from the first response instead of a second range
  # request for metadata parked at the end of the file.
  echo "==> Crossfading ${XFADE}s tail-into-head for a seamless loop..."

  # The H.264 level bounds macroblocks/frame *and* macroblocks/sec — a level
  # too low for the actual resolution+frame-rate isn't just a cosmetic
  # libx264 warning, it can make a strict hardware decoder (iOS
  # VideoToolbox included) refuse the stream outright. A hardcoded 4.1 is
  # wrong for anything shot at 50/60fps: a 1080x1920 clip is already at
  # Level 4.1's ceiling at ~30fps, so a 60fps source needs Level 4.2+. Pick
  # the lowest level that actually fits this clip's real macroblock rate,
  # widest device compatibility for the common case, correct for outliers.
  MBS=$(( ( (WIDTH + 15) / 16 ) * ( (HEIGHT + 15) / 16 ) ))
  MBPS=$(awk -v mbs="$MBS" -v fps="$FPS" 'BEGIN { split(fps, a, "/"); printf "%.0f", mbs * (a[1] / a[2]) }')
  LEVEL=$(awk -v mbs="$MBS" -v mbps="$MBPS" '
    BEGIN {
      n = split("10 11 12 13 20 21 22 30 31 32 40 41 42 50 51 52 60 61 62", tag, " ")
      split("1485 3000 6000 11880 11880 19800 20250 40500 108000 216000 245760 245760 522240 589824 983040 2073600 4177920 8355840 16711680", maxmbps, " ")
      split("99 396 396 396 396 792 1620 1620 3600 5120 8192 8192 8704 22080 36864 36864 139264 139264 139264", maxfs, " ")
      for (i = 1; i <= n; i++) {
        if (mbs <= maxfs[i] && mbps <= maxmbps[i]) { printf "%s", substr(tag[i],1,1) "." substr(tag[i],2); exit }
      }
      print "6.2"  # fell through every level: use the ceiling and let x264 warn
    }')
  echo "    (H.264 level ${LEVEL} for ${WIDTH}x${HEIGHT} @ ${FPS}fps — ${MBPS} MB/s)"

  ffmpeg -y -loglevel error -i "$TMP_TRIMMED" -filter_complex "
    [0:v]trim=0:${XFADE},setpts=PTS-STARTPTS,fps=${FPS}[head];
    [0:v]trim=${REM}:${DURATION},setpts=PTS-STARTPTS,fps=${FPS}[tail];
    [tail][head]xfade=transition=fade:duration=${XFADE}:offset=0[joined];
    [0:v]trim=${XFADE}:${REM},setpts=PTS-STARTPTS[mid];
    [joined][mid]concat=n=2:v=1:a=0,format=yuv420p,fps=${FPS}[out]
  " -map "[out]" -an -c:v libx264 -preset medium -profile:v high -level "$LEVEL" -crf 18 \
    -avoid_negative_ts make_zero -movflags +faststart "$OUT"
fi

echo "==> Wrote $OUT"
echo "    Test it: curl -f \"http://localhost:4534/rest/getTrackVideo?id=${ID}&u=USER&t=TOKEN&s=SALT\" -o /tmp/check.mp4"
