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
import subprocess
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


def grab() -> Frame:
    """Capture the primary virtual screen."""
    with mss.mss() as sct:
        # monitors[0] is the union of all screens; [1] is the first real one.
        monitor = sct.monitors[1] if len(sct.monitors) > 1 else sct.monitors[0]
        raw = sct.grab(monitor)
    image = Image.frombytes("RGB", raw.size, raw.bgra, "raw", "BGRX")
    return Frame(image=image, width=image.width, height=image.height, scale=1.0)


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
