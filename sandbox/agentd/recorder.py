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
import capture

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

# Keys that modify the next keystroke rather than being one.
#
# keysym_to_string(Shift_L) returns "\x00", not None, so without this list a
# Shift press was recorded as a one-character keystroke: it split typing in two
# and put a NUL in the trace that the database then refused.
# Keysym numbers back to their names, for the keys that are not characters.
#
# Xlib.XK has string_to_keysym and keysym_to_string, and keysym_to_string only
# answers for Latin-1: Return comes back as "\r" and Escape as "\x1b", which is
# how a key step ended up named after a control character. The module's own
# XK_* constants are the reverse map, so build it once.
_KEYSYM_NAMES = {
    getattr(XK, _n): _n[3:] for _n in dir(XK) if _n.startswith("XK_")
}


_MODIFIER_KEYSYMS = frozenset(
    k
    for k in (
        XK.string_to_keysym(n)
        for n in (
            "Shift_L", "Shift_R", "Control_L", "Control_R", "Alt_L", "Alt_R",
            "Meta_L", "Meta_R", "Super_L", "Super_R", "Hyper_L", "Hyper_R",
            "Caps_Lock", "Num_Lock", "Scroll_Lock", "ISO_Level3_Shift",
            "Mode_switch",
        )
    )
    if k
)




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
            "extra": self._frame(),
        })

    def _on_key(self, event) -> None:
        keysym = self._local_display.keycode_to_keysym(event.detail, 0)
        shift = bool(event.state & X.ShiftMask)
        ctrl = bool(event.state & X.ControlMask)
        alt = bool(event.state & X.Mod1Mask)

        # Pressing Shift is not typing anything.
        #
        # keysym_to_string(Shift_L) returns "\x00" rather than None, which is one
        # character long, so it used to be recorded as a keystroke: a NUL in the
        # trace that split "file:" into "file" and ":" and then failed the whole
        # save, because Postgres will not store \u0000. Modifiers are state on
        # the keystroke that follows them, never events of their own.
        if keysym in _MODIFIER_KEYSYMS:
            return

        # Shifted punctuation is a different keysym, not an uppercase one.
        # ";" shifted is ":", and ";".upper() is ";" -- which is how a recorded
        # file:// URL came back as "file;//".
        name = XK.keysym_to_string(
            self._local_display.keycode_to_keysym(event.detail, 1 if shift else 0))
        if name is None:
            name = XK.keysym_to_string(keysym)
        if name is None:
            name = XK.keysym_to_string(self._local_display.keycode_to_keysym(event.detail, 1)) or ""

        # Whatever survived, it must be text. A control character here is a key
        # this mapping does not understand, and naming it by keycode at least
        # says so out loud.
        if len(name) == 1 and (ord(name) < 0x20 or ord(name) == 0x7F):
            name = _KEYSYM_NAMES.get(keysym) or f"keycode{event.detail}"

        if len(name) == 1 and not ctrl and not alt:
            key = name
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
        # A frame for the keys that mean something on their own -- Return,
        # ctrl+a, Escape -- and none for the letters in between. Somebody
        # typing a URL produces thirty keystrokes and one interesting picture.
        self._append({
            "type": "key", "key": key,
            "window": ctx["window"], "role": ctx["role"], "label": ctx["label"],
            "extra": self._frame() if len(key) > 1 else None,
        })

    def _frame(self) -> dict | None:
        """A small picture of the screen as it was when this happened.

        Recorded steps are coordinates and, where the application exposes one,
        an accessible label. Firefox exposes nothing, so a demonstration of
        using a browser compiled to "click at 690,121" -- which is enough for a
        replay on an identical screen and nothing at all for an agent trying to
        work out what it is aiming at. The picture is what makes the step
        describable later.

        Small on purpose: a demonstration has a handful of interesting moments
        and no need for any of them at full resolution.
        """
        try:
            b64, _ = capture.encode(capture.grab(), max_width=720, quality=55)
        except Exception:
            # A recording that loses its pictures is worth far more than one
            # that stops because a grab failed.
            return None
        return {"frame": b64}

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
