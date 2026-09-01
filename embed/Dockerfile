# AgentFleet embedding sidecar: the floor under semantic memory.
#
# Episodic memory search needs vectors. Providers that can embed (Ollama,
# OpenAI, Gemini) are preferred when configured; this sidecar is what makes a
# fleet with none of them — an Anthropic-only fleet, or one with no providers
# yet — still get semantic recall instead of the hashed keyword fallback.
#
# model2vec's potion-base-8M is the whole trick: static token embeddings
# distilled from a sentence transformer, ~30 MB, no GPU, microseconds per
# sentence on CPU. Weaker than a real transformer encoder and enormously
# better than a bag of words.
FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    HF_HOME=/models

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# The model is baked at build time so the container never needs the internet:
# an air-gapped fleet gets semantic memory too, and first-request latency is
# not a model download.
RUN python -c "from model2vec import StaticModel; StaticModel.from_pretrained('minishlab/potion-base-8M')"

COPY app.py .

EXPOSE 8081
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=4 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8081/healthz', timeout=4)" || exit 1

CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8081"]
