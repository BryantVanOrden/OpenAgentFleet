"""Embedding sidecar: text in, vectors out, no GPU, no API key.

One job: give episodic memory real semantic vectors on every fleet, including
the ones with no embedding-capable provider configured. The orchestrator
prefers a configured provider's embeddings (better quality) and falls back
here; without either it degrades to a hashed keyword index and says so.

The model is model2vec's potion-base-8M — static token embeddings distilled
from a sentence transformer. It is deliberately the small honest choice:
~30 MB, sub-millisecond per sentence on CPU, and meaningfully semantic
("sign in to the billing portal" finds "logged into the invoicing site"),
while a real transformer encoder would need a GPU or noticeable latency for
a marginal gain at memory-recall scale.
"""

from __future__ import annotations

import logging

from fastapi import FastAPI, HTTPException
from model2vec import StaticModel
from pydantic import BaseModel

log = logging.getLogger("embed")
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

MODEL_NAME = "minishlab/potion-base-8M"

app = FastAPI(title="OpenAgentFleet Embeddings")

# Loaded at import: the weights are baked into the image, so this is a disk
# read measured in milliseconds, and failing at boot beats failing on the
# first recall.
_model = StaticModel.from_pretrained(MODEL_NAME)
_dim = int(_model.dim) if hasattr(_model, "dim") else len(_model.encode(["probe"])[0])
log.info("model %s loaded, %d dimensions", MODEL_NAME, _dim)


class EmbedRequest(BaseModel):
    texts: list[str]


# A memory or a query is a sentence or a paragraph. A caller sending a novel
# per entry is misusing the index, and truncating beats an out-of-memory kill
# that takes every other caller's request down with it.
MAX_TEXTS = 256
MAX_CHARS = 8_000


@app.get("/healthz")
def healthz() -> dict:
    return {"status": "ok", "model": MODEL_NAME, "dim": _dim}


@app.get("/info")
def info() -> dict:
    return {"model": MODEL_NAME, "dim": _dim}


@app.post("/embed")
def embed(req: EmbedRequest) -> dict:
    if not req.texts:
        raise HTTPException(status_code=400, detail="texts is empty")
    if len(req.texts) > MAX_TEXTS:
        raise HTTPException(
            status_code=400,
            detail=f"at most {MAX_TEXTS} texts per request; batch the rest",
        )
    texts = [t[:MAX_CHARS] if len(t) > MAX_CHARS else t for t in req.texts]
    # Empty strings would embed to garbage; a single space keeps positions
    # aligned with the request so the caller's zip stays correct.
    texts = [t if t.strip() else " " for t in texts]

    vectors = _model.encode(texts)
    return {
        "model": MODEL_NAME,
        "dim": _dim,
        "vectors": [[float(x) for x in v] for v in vectors],
    }
