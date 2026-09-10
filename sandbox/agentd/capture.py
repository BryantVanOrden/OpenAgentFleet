"""Screen capture and change detection.

Two jobs:

  * produce a WebP frame small enough to be cheap as a model input but large
    enough that UI text stays legible (that trade-off is what `max_width` and
    `quality` control), and
  * produce a difference hash so the orchestrator can tell "the screen did not
    change" from "the screen changed slightly", which is the entire basis of
    stall detection.
"""

from __future__ import annotations

import base64
import io
import os
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass

import mss
from PIL import Image

# dHash grid. 9x8 greyscale pixels -> 64 comparison bits.
_HASH_W, _HASH_H = 9, 8


@dataclass
class Frame:
    image: Image.Image
    width: int          # full desktop width
    height: int         # full desktop height
    scale: float        # encoded width / desktop width
    # Top-left of this frame in desktop coordinates. Non-zero only for crops —
    # the caller needs it to translate a click inside a zoomed view back to the
    # desktop.
    origin: tuple[int, int] = (0, 0)


class DisplayUnresponsive(RuntimeError):
    """The X server did not answer a screenshot request in time."""


# An X request that never returns cannot be interrupted from Python, and on
# 2026-09-09 one took agentd down: the server wedged, every observe blocked
# on its screenshot, the worker threads filled up and health stopped
# answering. So the screenshot is taken in a child (capture_child.py) the
# parent can kill, one at a time -- a second observe arriving while the first
# is stuck fails at once instead of joining the queue.
INPROC = os.environ.get("AGENTD_CAPTURE_INPROC") == "1"
CHILD_TIMEOUT = float(os.environ.get("AGENTD_CAPTURE_TIMEOUT", "10"))
_grab_lock = threading.Lock()
_unresponsive_since: float | None = None


def display_unresponsive() -> bool:
    return _unresponsive_since is not None


def _grab_inproc() -> Frame:
    with mss.mss() as sct:
        # monitors[0] is the union of all screens; [1] is the first real one.
        monitor = sct.monitors[1] if len(sct.monitors) > 1 else sct.monitors[0]
        raw = sct.grab(monitor)
    image = Image.frombytes("RGB", raw.size, raw.bgra, "raw", "BGRX")
    return Frame(image=image, width=image.width, height=image.height, scale=1.0)


def grab() -> Frame:
    """Capture the primary virtual screen.

    Raises DisplayUnresponsive when the server does not answer in time, or when
    another capture is already stuck waiting for it.
    """
    global _unresponsive_since
    if INPROC:
        return _grab_inproc()
    # A second caller waits for the capture in flight rather than being
    # turned away: the orchestrator's loop, a peer question about the screen
    # and a health probe legitimately overlap. It waits at most one capture
    # timeout, so a wedged display still fails everyone quickly.
    if not _grab_lock.acquire(timeout=CHILD_TIMEOUT):
        raise DisplayUnresponsive("a screenshot has been waiting on the display for "
                                  f"{CHILD_TIMEOUT:.0f}s")
    try:
        script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "capture_child.py")
        fd, path = tempfile.mkstemp(prefix="agentd-frame-", suffix=".png",
                                    dir="/dev/shm" if os.path.isdir("/dev/shm") else None)
        os.close(fd)
        try:
            try:
                proc = subprocess.run(
                    [sys.executable, script, path], capture_output=True, text=True,
                    timeout=CHILD_TIMEOUT, env=dict(os.environ, AGENTD_CAPTURE_INPROC="1"),
                )
            except subprocess.TimeoutExpired as exc:
                _unresponsive_since = _unresponsive_since or time.time()
                raise DisplayUnresponsive(f"no screenshot in {CHILD_TIMEOUT:.0f}s") from exc
            if proc.returncode != 0:
                raise RuntimeError(f"screenshot failed: {proc.stderr.strip()[:200]}")
            image = Image.open(path)
            image.load()
        finally:
            try:
                os.unlink(path)
            except OSError:
                pass
        _unresponsive_since = None
        return Frame(image=image.convert("RGB"), width=image.width, height=image.height, scale=1.0)
    finally:
        _grab_lock.release()


def crop(frame: Frame, region: tuple[int, int, int, int]) -> Frame:
    """Full-resolution crop of a screen region.

    Downscaling a 1920px desktop to 1280px to keep token cost sane is what makes
    small UI text — a filename in a list, a value in a form field — unreadable to
    the model. Cropping instead of scaling gives back the lost detail for the one
    region that matters, at a fraction of the tokens a full-resolution frame
    would cost.
    """
    x0, y0, x1, y1 = region
    # Clamp into the frame and enforce a sane minimum, so a degenerate region
    # from the model produces a small picture rather than an exception.
    x0 = max(0, min(int(x0), frame.width - 1))
    y0 = max(0, min(int(y0), frame.height - 1))
    x1 = max(x0 + 16, min(int(x1), frame.width))
    y1 = max(y0 + 16, min(int(y1), frame.height))

    cropped = frame.image.crop((x0, y0, x1, y1))
    return Frame(
        image=cropped,
        width=cropped.width,
        height=cropped.height,
        scale=1.0,
        origin=(x0, y0),
    )


def encode(frame: Frame, max_width: int = 1280, quality: int = 70) -> tuple[str, float]:
    """Downscale and WebP-encode a frame. Returns (base64, scale)."""
    image = frame.image
    scale = 1.0
    if max_width and image.width > max_width:
        scale = max_width / image.width
        image = image.resize(
            (max_width, max(1, int(image.height * scale))),
            # LANCZOS keeps small UI text readable in a way BILINEAR does not.
            Image.Resampling.LANCZOS,
        )
    buf = io.BytesIO()
    image.save(buf, format="WEBP", quality=quality, method=4)
    return base64.b64encode(buf.getvalue()).decode("ascii"), scale


def dhash(frame: Frame) -> str:
    """Difference hash of the frame, as hex.

    Insensitive to cursor blink and antialiasing noise, sensitive to a dialog
    opening or a page navigating — which is exactly the line stall detection
    needs to draw.
    """
    small = frame.image.convert("L").resize((_HASH_W, _HASH_H), Image.Resampling.LANCZOS)
    pixels = list(small.getdata())
    bits = 0
    index = 0
    for row in range(_HASH_H):
        offset = row * _HASH_W
        for col in range(_HASH_W - 1):
            if pixels[offset + col] > pixels[offset + col + 1]:
                bits |= 1 << index
            index += 1
    return f"{bits:016x}"


def active_window() -> str:
    """Title of the focused window, empty when nothing has focus."""
    try:
        out = subprocess.run(
            ["xdotool", "getactivewindow", "getwindowname"],
            capture_output=True, text=True, timeout=3,
        )
        if out.returncode == 0:
            return out.stdout.strip()
    except (subprocess.SubprocessError, OSError):
        pass
    return ""


def screen_text() -> str:
    """Best-effort text on screen, used by wait_for and assert.

    Reads the accessibility tree rather than running OCR: it is faster, exact,
    and already available. Windows that expose nothing to AT-SPI (a canvas, a
    video, a game) are invisible here — a real limitation, not a bug.
    """
    from a11y import flatten_text  # local import keeps startup cheap

    return flatten_text()
