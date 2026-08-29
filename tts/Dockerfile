# CPU-only by design.
#
# The GPU on a host like this is already holding the vision model the fleet
# runs on, and pocket-tts with int8 quantisation is comfortably faster than
# realtime without it — measured 3.9x on a Ryzen 5800H, so a sentence finishes
# synthesising well before it could finish playing.
#
# Installing the CPU build of torch explicitly matters: the default wheel drags
# in the entire CUDA runtime and costs several gigabytes of image for hardware
# this service will never touch.
FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    HF_HOME=/models \
    OMP_NUM_THREADS=4

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .

RUN pip install --no-cache-dir torch==2.6.0 --index-url https://download.pytorch.org/whl/cpu \
    && pip install --no-cache-dir -r requirements.txt

COPY app.py .

# Weights live on a volume, not in the image: they are large, they are the same
# for every rebuild, and baking them in would mean re-downloading on every
# code change.
VOLUME ["/models"]

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=180s --retries=10 \
    CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

# One worker: the model is several hundred megabytes of resident state and a
# second copy would double it for no throughput gain on a CPU that is already
# saturated by one synthesis.
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8080", "--workers", "1"]
