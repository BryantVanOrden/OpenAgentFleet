"""Semantic demonstration recorder.

A human does the task once; this captures it at two levels at the same time:

  * the raw input stream (X RECORD extension: real key and button events, not a
    replay of what some widget thought happened), and
  * the accessibility context at the moment of each interaction — which element,
    with which role and label, in which window.

The second level is what makes the recording survivable. A trace of clicks at
coordinates is worthless a week later; "click the push button labelled Build in
the Godot Engine window" still works after the window moved.
"""

from __future__ import annotations

import threading
import time
from dataclasses import dataclass, field

import a11y

try:
    from Xlib import X, XK, display
    from Xlib.ext import record
    from Xlib.protocol import rq

    XLIB_AVAILABLE = True
except Exception:  # pragma: no cover
    XLIB_AVAILABLE = False

# Two clicks closer together than this at the same spot are one double-click.
DOUBLE_CLICK_WINDOW = 0.4
DOUBLE_CLICK_SLOP = 6  # pixels

BUTTON_NAMES = {1: "left", 2: "middle", 3: "right"}


@dataclass
class Recording:
    name: str
    started: float
    events: list[dict] = field(default_factory=list)


class Recorder:
    """Owns at most one active recording.

    The X RECORD extension needs its own display connection: the recording
    context blocks while it delivers events, so it cannot share the connection
    agentd uses for everything else.
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._current: Recording | None = None
        self._thread: threading.Thread | None = None
        self._ctx = None
        self._record_display = None
        self._local_display = None
        self._last_click: tuple[float, int, int] | None = None
        self._pointer = (0, 0)

    # ------------------------------------------------------------- lifecycle ---

    @property
    def active(self) -> bool:
        with self._lock:
            return self._current is not None

    def start(self, name: str) -> None:
        if not XLIB_AVAILABLE:
            raise RuntimeError("python-xlib is unavailable; cannot record")
        with self._lock:
            if self._current is not None:
                raise RuntimeError("a recording is already in progress")
            self._current = Recording(name=name, started=time.time())
            self._last_click = None

        self._thread = threading.Thread(target=self._capture_loop, daemon=True)
        self._thread.start()

    def stop(self) -> Recording:
        with self._lock:
            current = self._current
            self._current = None
        if current is None:
            raise RuntimeError("no recording in progress")

        # Tearing down the context is what unblocks the capture thread.
        if self._ctx is not None and self._local_display is not None:
            try:
                self._local_display.record_disable_context(self._ctx)
                self._local_display.flush()
            except Exception:
                pass
        if self._thread is not None:
            self._thread.join(timeout=5)
        self._ctx = None
        return current

    # --------------------------------------------------------------- capture ---

    def _capture_loop(self) -> None:
        try:
            self._local_display = display.Display()
            self._record_display = display.Display()
            if not self._record_display.has_extension("RECORD"):
                raise RuntimeError("X server has no RECORD extension")

            self._ctx = self._record_display.record_create_context(
                0,
                [record.AllClients],
                [{
                    "core_requests": (0, 0),
                    "core_replies": (0, 0),
                    "ext_requests": (0, 0, 0, 0),
                    "ext_replies": (0, 0, 0, 0),
                    "delivered_events": (0, 0),
                    "device_events": (X.KeyPress, X.MotionNotify),
                    "errors": (0, 0),
                    "client_started": False,
                    "client_died": False,
                }],
            )
            # Blocks until the context is disabled by stop().
            self._record_display.record_enable_context(self._ctx, self._handle_packet)
            self._record_display.record_free_context(self._ctx)
        except Exception as exc:  # pragma: no cover - environment dependent
            self._append({"type": "recorder_error", "extra": {"message": str(exc)}})

    def _handle_packet(self, reply) -> None:
        if reply.category != record.FromServer or reply.client_swapped:
            return
        if not len(reply.data) or reply.data[0] < 2:
            return

        data = reply.data
        while len(data):
            event, data = rq.EventField(None).parse_binary_value(
                data, self._record_display.display, None, None
            )
            if event.type == X.MotionNotify:
                self._pointer = (event.root_x, event.root_y)
            elif event.type == X.ButtonPress:
                self._on_button(event)
            elif event.type == X.KeyPress:
                self._on_key(event)

    def _on_button(self, event) -> None:
        x, y = event.root_x, event.root_y
        button = BUTTON_NAMES.get(event.detail)

        if button is None:
            # Buttons 4/5 are the scroll wheel.
            if event.detail in (4, 5):
                self._append({
                    "type": "scroll", "x": x, "y": y,
                    "extra": {"amount": "3" if event.detail == 4 else "-3"},
                })
            return

        now = time.time()
        if (
            button == "left"
            and self._last_click is not None
            and now - self._last_click[0] < DOUBLE_CLICK_WINDOW
            and abs(x - self._last_click[1]) <= DOUBLE_CLICK_SLOP
            and abs(y - self._last_click[2]) <= DOUBLE_CLICK_SLOP
        ):
            # Rewrite the previous click rather than emitting both: a model
            # replaying click-then-click is not the same gesture.
            self._replace_last_click_with_double()
            self._last_click = None
            return

        if button == "left":
            self._last_click = (now, x, y)

        ctx = self._context_at(x, y)
        self._append({
            "type": "click", "button": button, "x": x, "y": y,
            "window": ctx["window"], "role": ctx["role"], "label": ctx["label"],
        })

    def _on_key(self, event) -> None:
        keysym = self._local_display.keycode_to_keysym(event.detail, 0)
        name = XK.keysym_to_string(keysym)
        shift = bool(event.state & X.ShiftMask)
        ctrl = bool(event.state & X.ControlMask)
        alt = bool(event.state & X.Mod1Mask)

        if name is None:
            name = XK.keysym_to_string(self._local_display.keycode_to_keysym(event.detail, 1)) or ""

        if len(name) == 1 and not ctrl and not alt:
            key = name.upper() if shift else name
        else:
            parts = []
            if ctrl:
                parts.append("ctrl")
            if alt:
                parts.append("alt")
            if shift and len(name) > 1:
                parts.append("shift")
            parts.append(name or f"keycode{event.detail}")
            key = "+".join(parts)

        ctx = self._context_at(*self._pointer)
        self._append({
            "type": "key", "key": key,
            "window": ctx["window"], "role": ctx["role"], "label": ctx["label"],
        })

    # ---------------------------------------------------------------- helpers ---

    def _context_at(self, x: int, y: int) -> dict:
        node = a11y.at_point(x, y)
        window = a11y.window_title_at(x, y)
        return {
            "window": window,
            "role": node.role if node else "",
            "label": node.name if node else "",
        }

    def _append(self, event: dict) -> None:
        with self._lock:
            if self._current is None:
                return
            event["t"] = round(time.time() - self._current.started, 3)
            self._current.events.append(event)

    def _replace_last_click_with_double(self) -> None:
        with self._lock:
            if self._current is None or not self._current.events:
                return
            for i in range(len(self._current.events) - 1, -1, -1):
                if self._current.events[i].get("type") == "click":
                    self._current.events[i]["type"] = "double_click"
                    return


RECORDER = Recorder()
