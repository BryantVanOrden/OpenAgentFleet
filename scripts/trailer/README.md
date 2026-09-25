# Trailer

The source for the site's trailer: 74 seconds in 16:9, and a 36-second 9:16
cut for phones. Every frame is drawn by a web page from a time value, so a
render is exactly reproducible. Change a screenshot or a line and render
again.

| File | What it does |
|------|--------------|
| `comp/index.html` | The composition. `render(t)` draws the frame at time `t`: 3D scenes, captions, flashes, grain. |
| `comp/timeline.json` | Both cuts: scenes, captions, where each narration line starts, sound effects, hits. |
| `lines.json`, `vo/shadow/` | The narration script and the raw Pocket TTS takes the timeline is timed to. |
| `prepare.py` | Copies the screenshots and logos from `docs/images` and `site/assets` into `comp/a/`. |
| `render.py` | Renders frames with Playwright, several browsers in parallel. |
| `vo_fx.py`, `mix.py` | Turns the takes into the trailer voice, and mixes the music, narration and sound design. |
| `encode.sh` | Encodes the masters and the web versions. |
| `flicker.py`, `sheet.py`, `words.py`, `analyze.py` | Checks: one-frame glitches, contact sheets, phrase timings, music tempo and drops. |

## Render it

You need Python with `playwright` and `pillow` (`python -m playwright install chromium`)
and Docker. The audio tools run in a small image:

```bash
docker build -t oaf-av .
```

Put the music track at `music.mp3`. It is Apex_Momentum, and it isn't
committed. The timeline's music cuts (`"music"` in `timeline.json`) assume
its 80 BPM grid and its drops at 16.1, 88.1 and 96.1 s.

```bash
python prepare.py
python render.py h 6            # 2220 frames, about 3 minutes
python render.py v 6            # 1080 frames
python flicker.py frames_h      # should report 0 outlier frames
python flicker.py frames_v
docker run --rm -v "$PWD:/work" oaf-av python /work/vo_fx.py shadow 0.87
docker run --rm -v "$PWD:/work" oaf-av python /work/mix.py h
docker run --rm -v "$PWD:/work" oaf-av python /work/mix.py v
docker run --rm -v "$PWD:/work" oaf-av sh /work/encode.sh
```

On Git Bash, prefix the `docker run` lines with `MSYS_NO_PATHCONV=1` and use a
Windows path for the mount.

To check a few moments before a full render, save stills to `stills/`:

```bash
python render.py h stills 9.3,16.2,43.5
```

Copy `openagentfleet-trailer.mp4` and `openagentfleet-reel.mp4` into
`site/assets/video/`, and bump the `?v=` on their links in `site/index.html`:
Pages caches for ten minutes. `docs/images/trailer-poster.jpg` is the README's
poster, and `site/assets/video/trailer-poster.jpg` is the same frame with no
play button drawn on it.

## Change the narration

Edit `lines.json` and regenerate the lines you changed with
`AF_TOKEN=<console token> python tts.py shadow 1.0 key1,key2`. The fleet must
be running with voice enabled. Then run `vo_fx.py` and `words.py`. The phrase
timings `words.py` prints are what the caption times in `timeline.json` line
up to: a caption starts where its phrase starts, the VO start plus the
phrase offset.

## Rules the page follows

- **`render(t)` depends only on `t`.** Each browser renders every Nth frame,
  so anything measured from the previous frame is wrong there. It shows as a
  one-frame jump.
- **Every image has loaded before rendering starts.** `render.py` refuses to
  start otherwise. A missed image shows up as a broken icon in every frame that
  browser renders.
- **Grain stays faint (3.5%).** Film grain is the most expensive thing in the
  frame for H.264.
- **The voice sits over the music.** `mix.py` ducks the music's 180 Hz–5 kHz
  band hard under the voice and prints the margin for each line. Aim for 7 dB
  or more.
