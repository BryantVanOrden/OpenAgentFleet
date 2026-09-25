"""Narration lines through Pocket TTS (the fleet's /api/voice/speak).

    AF_TOKEN=<console token> python tts.py [voice] [speed] [only,these,keys]

AF_API defaults to http://localhost:8080. Writes vo/<voice>/<key>.wav.
"""
import json, os, sys, urllib.request, wave

S = os.path.dirname(os.path.abspath(__file__))
TOKEN = os.environ["AF_TOKEN"]
API = os.environ.get("AF_API", "http://localhost:8080").rstrip("/")
LINES = json.load(open(os.path.join(S, "lines.json"), encoding="utf-8"))
voice = sys.argv[1] if len(sys.argv) > 1 else "shadow"
speed = float(sys.argv[2]) if len(sys.argv) > 2 else 1.0
only = set(sys.argv[3].split(",")) if len(sys.argv) > 3 else None
out = os.path.join(S, "vo", voice)
os.makedirs(out, exist_ok=True)
durs = {}
for key, text in LINES.items():
    if only and key not in only:
        continue
    body = json.dumps({"text": text, "voice": voice, "speed": speed}).encode()
    req = urllib.request.Request(API + "/api/voice/speak", data=body, method="POST",
                                 headers={"Authorization": "Bearer " + TOKEN, "Content-Type": "application/json"})
    data = urllib.request.urlopen(req, timeout=120).read()
    p = os.path.join(out, key + ".wav")
    open(p, "wb").write(data)
    with wave.open(p) as w:
        durs[key] = round(w.getnframes() / w.getframerate(), 2)
    print(f"{key:10s} {durs[key]:5.2f}s  {text}")
json.dump(durs, open(os.path.join(out, "durations.json"), "w"), indent=1)
