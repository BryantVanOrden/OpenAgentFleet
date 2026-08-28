"""agentd — the sandbox-side control plane.

The only component with access to the virtual input devices, the screen and the
accessibility bus. The orchestrator can observe and act only through this API,
which keeps the trust boundary in one auditable place.

It listens on the sandbox network with no authentication of its own, exactly like
a kubelet: the port is never published to the host, the docker network is not
routable from outside, and the orchestrator's proxy is what carries operator
authentication. If you publish this port, you have given away the desktop.
"""

from __future__ import annotations

import os
import time
from datetime import datetime, timezone
# Pydantic resolves annotations lazily under `from __future__ import
# annotations`, so a missing name here does not fail at import — it fails when
# the model is first validated, i.e. on every /act call, at run time.
from typing import Any

from fastapi import FastAPI, HTTPException
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

import a11y
import capture
import inject
from recorder import RECORDER

ALLOW_SHELL = os.environ.get("ALLOW_SHELL", "false").lower() in ("1", "true", "yes")
INSTANCE_ID = os.environ.get("AGENTFLEET_INSTANCE", "")
KEYRING_DIR = "/var/run/agentfleet/keyring"

app = FastAPI(title="agentd", docs_url=None, redoc_url=None, openapi_url=None)


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


# --------------------------------------------------------------------- health ---


@app.get("/health")
def health() -> dict:
    # A frame grab is the real readiness signal: X can be up while the desktop
    # session is still starting, and an agent handed a black screen wastes steps.
    try:
        frame = capture.grab()
        ready = frame.width > 0 and frame.height > 0
    except Exception as exc:
        raise HTTPException(status_code=503, detail=f"display not ready: {exc}") from exc

    return {
        "status": "ok" if ready else "starting",
        "instance": INSTANCE_ID,
        "resolution": f"{frame.width}x{frame.height}",
        "a11y": a11y.AVAILABLE,
        "shell": ALLOW_SHELL,
        "recording": RECORDER.active,
        "time": _now(),
    }


# -------------------------------------------------------------------- observe ---


class ObserveRequest(BaseModel):
    screenshot: bool = True
    a11y: bool = True
    max_width: int = Field(default=1280, ge=320, le=3840)
    quality: int = Field(default=70, ge=20, le=100)
    som: bool = True
    # [x0, y0, x1, y1] in desktop pixels. When set, the returned image is a
    # full-resolution crop of that region instead of a downscaled full desktop.
    region: list[int] | None = None


@app.post("/observe")
def observe(req: ObserveRequest) -> dict:
    full = capture.grab()

    # The hash is always taken over the FULL desktop. Computing it over a crop
    # would make stall detection blind to anything happening outside the zoom,
    # which is exactly when an agent is most likely to be stuck.
    screen_hash = capture.dhash(full)

    frame = full
    if req.region and len(req.region) == 4:
        frame = capture.crop(full, tuple(req.region))  # type: ignore[arg-type]

    marks = []
    if req.som and req.screenshot:
        try:
            from som import annotate_frame
            nodes = a11y.snapshot() if req.a11y else []
            if nodes:
                annotated_img, marks = annotate_frame(frame.image, nodes)
                frame = capture.Frame(
                    image=annotated_img,
                    width=frame.width,
                    height=frame.height,
                    scale=frame.scale,
                    origin=frame.origin,
                )
        except Exception:
            pass

    shot, scale = ("", 1.0)
    if req.screenshot:
        shot, scale = capture.encode(frame, req.max_width, req.quality)

    tree = a11y.flatten() if req.a11y else ""

    return {
        "screenshot_b64": shot,
        # Dimensions of the image actually returned, so a caller can map a click
        # in image space back to the desktop without guessing.
        "width": frame.width,
        "height": frame.height,
        "scale": scale,
        "origin_x": frame.origin[0],
        "origin_y": frame.origin[1],
        "desktop_width": full.width,
        "desktop_height": full.height,
        "active_window": capture.active_window(),
        "a11y_tree": tree,
        "marks": marks,
        "hash": screen_hash,
        "captured_at": _now(),
    }


# ------------------------------------------------------------------------ act ---


class ActRequest(BaseModel):
    action: str
    target: str | None = None
    role: str | None = None
    mark: int | None = None
    coordinates: list[int] | None = None
    to: list[int] | None = None
    text: str | None = None
    code: str | None = None
    query: str | None = None
    tool_name: str | None = None
    tool_description: str | None = None
    tool_parameters: dict[str, Any] | None = None
    tool_handler: str | None = None
    key: str | None = None
    amount: int = 0
    timeout: int = 120
    # Present in the shared action schema but meaningless to agentd.
    thought: str | None = None
    question: str | None = None
    summary: str | None = None
    sub_goal: str | None = None
    wait_child: bool = True


def _coords(req: ActRequest) -> tuple[int, int] | None:
    """Resolve where to act: an accessible label wins over raw coordinates."""
    if req.target:
        hit = inject.resolve_target(req.target, req.role)
        if hit is not None:
            return hit
    if req.coordinates and len(req.coordinates) == 2:
        return int(req.coordinates[0]), int(req.coordinates[1])
    return None


@app.post("/act")
def act(req: ActRequest) -> dict:
    kind = req.action.lower()

    if kind in ("click", "double_click", "right_click"):
        point = _coords(req)
        if point is None:
            return {
                "ok": False,
                "detail": f"could not locate {req.target!r} and no coordinates were given",
            }
        button = 3 if kind == "right_click" else 1
        clicks = 2 if kind == "double_click" else 1
        ok, detail = inject.click(point[0], point[1], button=button, clicks=clicks)
        if ok and req.target:
            detail += f" ({req.target!r})"
        return {"ok": ok, "detail": detail}

    if kind == "type":
        if not req.text:
            return {"ok": False, "detail": "no text supplied"}
        # A target means "put the text in this field": focus it first.
        if req.target:
            point = _coords(req)
            if point is not None:
                inject.click(point[0], point[1])
        ok, detail = inject.type_text(req.text)
        return {"ok": ok, "detail": detail}

    if kind == "key":
        if not req.key:
            return {"ok": False, "detail": "no key supplied"}
        ok, detail = inject.key(req.key)
        return {"ok": ok, "detail": detail}

    if kind == "scroll":
        point = _coords(req) or (capture.grab().width // 2, capture.grab().height // 2)
        ok, detail = inject.scroll(point[0], point[1], req.amount or 3)
        return {"ok": ok, "detail": detail}

    if kind == "drag":
        start = _coords(req)
        if start is None or not req.to or len(req.to) != 2:
            return {"ok": False, "detail": "drag needs coordinates and to"}
        ok, detail = inject.drag(start[0], start[1], int(req.to[0]), int(req.to[1]))
        return {"ok": ok, "detail": detail}

    if kind == "focus":
        title = req.target or req.text or ""
        if not title:
            return {"ok": False, "detail": "focus needs a window title"}
        ok, detail = inject.focus_window(title)
        return {"ok": ok, "detail": detail}

    if kind == "wait":
        seconds = max(1, min(req.amount or 3, 300))
        time.sleep(seconds)
        return {"ok": True, "detail": f"waited {seconds}s"}

    if kind == "wait_for":
        if not req.text:
            return {"ok": False, "detail": "wait_for needs text"}
        matched, detail = inject.wait_for_text(req.text, req.timeout or 120)
        return {"ok": True, "matched": matched, "detail": detail}

    if kind == "assert":
        if not req.text:
            return {"ok": False, "detail": "assert needs an expression"}
        matched, detail = inject.assert_condition(req.text, ALLOW_SHELL)
        return {"ok": True, "matched": matched, "detail": detail}

    if kind == "shell":
        if not ALLOW_SHELL:
            return {"ok": False, "detail": "shell execution is disabled for this instance",
                    "exit_code": 126}
        if not req.text:
            return {"ok": False, "detail": "shell needs a command"}
        ok, out, code = inject.shell(req.text, ALLOW_SHELL, timeout=min(req.timeout or 600, 3600))
        return {"ok": True, "detail": f"exit {code}", "stdout": out, "exit_code": code}

    # Second enforcement point for code execution. The orchestrator gates these
    # too, but agentd is the component that actually holds the interpreter, so
    # it refuses on its own authority rather than trusting its caller.
    if kind in ("python", "mount_tool", "call_tool") and not ALLOW_SHELL:
        return {
            "ok": False,
            "detail": f"{kind} is disabled for this instance (code execution is off)",
            "exit_code": 126,
        }

    if kind == "python":
        from repl import REPL
        code = req.code or req.text
        if not code:
            return {"ok": False, "detail": "python action needs code"}
        ok, out = REPL.execute(code, timeout=min(req.timeout or 120, 600))
        return {"ok": ok, "detail": "python executed", "stdout": out}

    if kind == "mount_tool":
        from repl import REPL
        name = req.tool_name or req.target or ""
        handler = req.tool_handler or req.code or req.text or ""
        if not name or not handler:
            return {"ok": False, "detail": "mount_tool requires tool_name and tool_handler code"}
        ok, out = REPL.mount_tool(name, req.tool_description or "", req.tool_parameters, handler)
        return {"ok": ok, "detail": out, "stdout": out}

    if kind == "unmount_tool":
        from repl import REPL
        name = req.tool_name or req.target or req.text or ""
        if not name:
            return {"ok": False, "detail": "unmount_tool requires tool_name"}
        ok, out = REPL.unmount_tool(name)
        return {"ok": ok, "detail": out, "stdout": out}

    if kind == "call_tool":
        from repl import REPL
        name = req.tool_name or req.target or ""
        if not name:
            return {"ok": False, "detail": "call_tool requires tool_name"}
        ok, out = REPL.call_tool(name, req.tool_parameters)
        return {"ok": ok, "detail": f"tool {name} executed", "stdout": out}

    if kind == "deep_search":
        from search import search_web
        q = req.query or req.text or req.sub_goal or ""
        if not q:
            return {"ok": False, "detail": "deep_search requires a query"}
        ok, out = search_web(q)
        return {"ok": ok, "detail": "search completed", "stdout": out}

    if kind == "speak":
        from voice import VOICE, DEFAULT_VOICE
        import base64
        txt = req.text or req.sub_goal or ""
        v = req.target or DEFAULT_VOICE
        wav_bytes = VOICE.synthesize_wav(txt, v)
        b64 = base64.b64encode(wav_bytes).decode("ascii")
        return {"ok": True, "detail": f"spoken with voice '{v}'", "audio_b64": b64, "format": "audio/wav"}

    return {"ok": False, "detail": f"unsupported action {kind!r}"}


# ---------------------------------------------------------------------- voice ---


class SpeakRequest(BaseModel):
    text: str
    voice: str = "shadow"


@app.get("/voice/voices")
def list_voices() -> dict:
    from voice import VOICE, DEFAULT_VOICE
    return {"voices": VOICE.list_voices(), "default": DEFAULT_VOICE}


@app.post("/voice/speak")
def voice_speak(req: SpeakRequest) -> dict:
    from voice import VOICE, DEFAULT_VOICE
    import base64
    v = req.voice or DEFAULT_VOICE
    wav_bytes = VOICE.synthesize_wav(req.text, v)
    b64 = base64.b64encode(wav_bytes).decode("ascii")
    return {"voice": v, "audio_b64": b64, "format": "audio/wav"}


# ------------------------------------------------------------------- recorder ---


class RecordStart(BaseModel):
    name: str = "Untitled recording"


@app.post("/record/start")
def record_start(req: RecordStart) -> dict:
    try:
        RECORDER.start(req.name)
    except RuntimeError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    return {"status": "recording", "name": req.name}


@app.post("/record/stop")
def record_stop() -> dict:
    try:
        rec = RECORDER.stop()
    except RuntimeError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    return {"name": rec.name, "events": rec.events, "frames": []}


# -------------------------------------------------------------------- keyring ---


class KeyringEntry(BaseModel):
    name: str
    value: str


@app.post("/keyring")
def keyring_put(entry: KeyringEntry) -> JSONResponse:
    """Store a credential for the duration of a run.

    Files land in a tmpfs-backed directory readable only by the agent user, so a
    secret is available to a build script or a browser profile without ever being
    written to the image, the prompt history, or the audit log.
    """
    if "/" in entry.name or entry.name.startswith("."):
        raise HTTPException(status_code=400, detail="invalid keyring name")
    os.makedirs(KEYRING_DIR, mode=0o700, exist_ok=True)
    path = os.path.join(KEYRING_DIR, entry.name)
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(entry.value)
    os.chmod(path, 0o600)
    return JSONResponse(status_code=204, content=None)


@app.delete("/keyring")
def keyring_clear() -> JSONResponse:
    if os.path.isdir(KEYRING_DIR):
        for name in os.listdir(KEYRING_DIR):
            try:
                os.remove(os.path.join(KEYRING_DIR, name))
            except OSError:
                pass
    return JSONResponse(status_code=204, content=None)
