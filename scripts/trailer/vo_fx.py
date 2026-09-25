"""Trailer voice: trim, pitch down, weight, presence, glue, a little room."""
import glob, json, os, subprocess, sys
import numpy as np
def load(path, sr=16000):
    raw = subprocess.run(["ffmpeg", "-v", "error", "-i", path, "-ac", "1", "-ar", str(sr), "-f", "s16le", "-"],
                         capture_output=True, check=True).stdout
    return np.frombuffer(raw, dtype=np.int16).astype(np.float32) / 32768.0, sr


def f0_median(x, sr):
    frame, hop = int(0.04 * sr), int(0.01 * sr)
    f0s = []
    for i in range(0, len(x) - frame, hop):
        seg = x[i:i + frame] - x[i:i + frame].mean()
        if np.sqrt((seg ** 2).mean()) < 0.02:
            continue
        ac = np.correlate(seg, seg, "full")[frame - 1:]
        lo, hi = int(sr / 300), int(sr / 60)
        k = lo + np.argmax(ac[lo:hi])
        if ac[k] > 0.4 * ac[0]:
            f0s.append(sr / k)
    return float(np.median(f0s)) if f0s else 0.0, len(f0s)



voice = sys.argv[1] if len(sys.argv) > 1 else "shadow"
pitch = float(sys.argv[2]) if len(sys.argv) > 2 else 0.87
src = f"/work/vo/{voice}"
dst = "/work/vo/final"
os.makedirs(dst, exist_ok=True)
CHAIN = ",".join([
    "silenceremove=start_periods=1:start_threshold=-45dB:start_silence=0.02",
    "areverse", "silenceremove=start_periods=1:start_threshold=-45dB:start_silence=0.05", "areverse",
    f"rubberband=pitch={pitch}:transients=smooth:detector=compound:phase=laminar:window=standard:smoothing=on",
    "highpass=f=55",
    "equalizer=f=105:t=o:w=1.1:g=4.5",
    "equalizer=f=320:t=o:w=1.0:g=-2.5",
    "equalizer=f=3200:t=o:w=1.2:g=3",
    "acompressor=threshold=-22dB:ratio=4:attack=4:release=90:makeup=5",
    "aecho=0.85:0.6:38|71:0.16|0.10",
    "apad=pad_dur=0.25",
    "alimiter=limit=0.9",
    "aresample=48000", "aformat=channel_layouts=stereo",
])
durs = {}
for p in sorted(glob.glob(src + "/*.wav")):
    k = os.path.basename(p)[:-4]
    o = f"{dst}/{k}.wav"
    subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", p, "-af", CHAIN, o], check=True)
    x, sr = load(o)
    f0, _ = f0_median(x, sr)
    durs[k] = round(len(x) / sr, 2)
    print(f"{k:10s} {durs[k]:5.2f}s f0 {f0:5.1f}")
json.dump(durs, open(dst + "/durations.json", "w"), indent=1)
