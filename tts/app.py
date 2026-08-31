"""Text to speech for AgentFleet, on CPU.

Runs beside the orchestrator rather than inside it or inside a sandbox. The
orchestrator is a Go binary in an Alpine image and cannot host a PyTorch model;
a sandbox is the wrong side of the fence, because the app talks to the
orchestrator and never to a sandbox, every agent would need its own copy of the
weights, and audio produced there has no way back out. One service here means
one set of weights, one warm model, and the same voice for every agent.

Deliberately CPU-only. The GPU on this class of host is already holding the
vision model the fleet runs on, and pocket-tts int8 is comfortably faster than
realtime without it — measured 3.9x on this machine, so a sentence is spoken
before it could finish playing.
"""

from __future__ import annotations

import io
import logging
import os
import threading
import time
import wave

import torch
from fastapi import FastAPI, HTTPException
from fastapi.responses import Response
from pydantic import BaseModel, Field

from pocket_tts import TTSModel
from pocket_tts.models.tts_model import get_predefined_voice

log = logging.getLogger("tts")
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

LANGUAGE = os.getenv("TTS_LANGUAGE", "english")
SAMPLE_RATE = 24_000

# The six names the fleet has always offered. They used to select a base
# frequency for a sine-wave generator, which is why every "voice" sounded the
# same; they now map to actual pocket-tts speakers, chosen to be audibly
# distinct from one another rather than to match the old tones.
VOICE_ALIASES: dict[str, str] = {
    "shadow": "michael",
    "atlas": "stuart_bell",
    "vortex": "charles",
    "echo": "jane",
    "aura": "alba",
    "lyra": "eve",
}

# The shipped default, named once so the resolver, the catalogue and the
# warm-up cannot disagree about it. They did: resolve_voice fell back to "echo"
# while every document and picker said "shadow".
DEFAULT_VOICE = "shadow"

ALIAS_BLURB = {
    "shadow": "Low and level",
    "atlas": "Warm, unhurried",
    "vortex": "Bright and quick",
    "echo": "Clear, neutral",
    "aura": "Soft, higher register",
    "lyra": "Light and articulate",
}

# Everything the model ships for this language, so the picker is not limited to
# the six presets.
ALL_VOICES = [
    "alba", "anna", "azelma", "bill_boerst", "caro_davy", "charles", "cosette",
    "eponine", "estelle", "eve", "fantine", "george", "giovanni", "jane",
    "javert", "jean", "juergen", "lola", "marius", "mary", "michael", "paul",
    "peter_yearsley", "rafael", "stuart_bell", "vera",
]

app = FastAPI(title="AgentFleet TTS")

_model: TTSModel | None = None
_model_lock = threading.Lock()
# Building a voice's conditioning state costs ~2s and never changes, so it is
# built once per voice and kept.
_voice_states: dict[str, dict] = {}
_voice_lock = threading.Lock()


def model() -> TTSModel:
    global _model
    if _model is None:
        with _model_lock:
            if _model is None:
                t0 = time.time()
                # quantize=True is int8 dynamic quantisation of the flow LM.
                # It is what makes this workable on CPU.
                _model = TTSModel.load_model(language=LANGUAGE, quantize=True)
                log.info("model loaded in %.1fs", time.time() - t0)
    return _model


def resolve_voice(name: str | None) -> str:
    """Map an alias or a raw speaker name onto a real speaker."""
    key = (name or "").strip().lower()
    if not key:
        # "shadow" is the documented default -- it is what the docs, the
        # console's picker and the sandbox's voice table all name. This returned
        # "echo", so which voice you actually got depended on whether the caller
        # bothered to fill the field in, and the two sounded nothing alike.
        return VOICE_ALIASES[DEFAULT_VOICE]
    if key in VOICE_ALIASES:
        return VOICE_ALIASES[key]
    if key in ALL_VOICES:
        return key
    raise HTTPException(status_code=400, detail=f"unknown voice {name!r}")


def voice_state(speaker: str) -> dict:
    with _voice_lock:
        state = _voice_states.get(speaker)
    if state is not None:
        return state

    t0 = time.time()
    built = model().get_state_for_audio_prompt(get_predefined_voice(LANGUAGE, speaker))
    log.info("voice %s prepared in %.1fs", speaker, time.time() - t0)
    with _voice_lock:
        _voice_states[speaker] = built
    return built


def to_wav(audio: torch.Tensor, speed: float) -> bytes:
    """Float samples to a 16-bit WAV.

    Speed is applied by changing the playback rate in the header rather than
    resampling: it costs nothing, and for a ±30% nudge the pitch shift reads as
    the speaker talking faster rather than as a defect.
    """
    samples = audio.detach().to(torch.float32).flatten()
    # Clip before scaling; a stray sample above 1.0 would wrap to full-scale
    # noise as int16.
    samples = samples.clamp(-1.0, 1.0)
    pcm = (samples * 32767.0).to(torch.int16).numpy().tobytes()

    rate = int(SAMPLE_RATE * max(0.5, min(2.0, speed or 1.0)))
    buf = io.BytesIO()
    with wave.open(buf, "wb") as wf:
        wf.setnchannels(1)
        wf.setsampwidth(2)
        wf.setframerate(rate)
        wf.writeframes(pcm)
    return buf.getvalue()


class SpeakRequest(BaseModel):
    text: str = Field(min_length=1, max_length=4000)
    voice: str | None = None
    speed: float = 1.0


@app.get("/healthz")
def healthz() -> dict:
    return {"status": "ok", "language": LANGUAGE, "loaded": _model is not None}


@app.get("/voices")
def voices() -> list[dict]:
    """Preset names first, then every other speaker the model ships."""
    out = [
        {
            "id": alias,
            "name": alias.capitalize(),
            "description": ALIAS_BLURB.get(alias, ""),
            "speaker": speaker,
            "preset": True,
            # Marked so a picker can show which one it will get if it asks for
            # nothing, rather than each client hardcoding its own guess.
            "default": alias == DEFAULT_VOICE,
        }
        for alias, speaker in VOICE_ALIASES.items()
    ]
    taken = set(VOICE_ALIASES.values())
    out += [
        {
            "id": s,
            "name": s.replace("_", " ").title(),
            "description": "",
            "speaker": s,
            "preset": False,
        }
        for s in ALL_VOICES
        if s not in taken
    ]
    return out


@app.post("/speak")
def speak(req: SpeakRequest) -> Response:
    speaker = resolve_voice(req.voice)
    text = req.text.strip()
    if not text:
        raise HTTPException(status_code=400, detail="text is required")

    t0 = time.time()
    try:
        state = voice_state(speaker)
        # Roughly 12.5 frames a second; this bounds a runaway generation
        # without truncating anything of a sane length.
        audio = model().generate_audio(state, text, max_tokens=1200)
    except HTTPException:
        raise
    except Exception as exc:  # noqa: BLE001 - reported to the caller verbatim
        log.exception("synthesis failed")
        raise HTTPException(status_code=500, detail=f"synthesis failed: {exc}") from exc

    wav = to_wav(audio, req.speed)
    dur = audio.shape[-1] / SAMPLE_RATE
    took = time.time() - t0
    log.info(
        "spoke %d chars as %s: %.2fs for %.1fs audio (%.1fx realtime)",
        len(text), speaker, took, dur, dur / took if took else 0,
    )
    return Response(content=wav, media_type="audio/wav")


@app.on_event("startup")
def warm() -> None:
    """Load the model and the default voice at boot.

    Without this the first person to press play waits for a cold model on top
    of their own synthesis, which reads as the feature being slow rather than
    as a one-off.
    """
    def _warm() -> None:
        try:
            voice_state(resolve_voice(None))
            log.info("warm and ready")
        except Exception:  # noqa: BLE001
            log.exception("warm-up failed; first request will pay the cost")

    threading.Thread(target=_warm, daemon=True).start()
