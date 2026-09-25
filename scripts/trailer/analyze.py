"""Voice pitch per candidate, and the music's tempo, beats and energy."""
import glob, json, os, subprocess
import numpy as np
from scipy.io import wavfile
from scipy.signal import find_peaks


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


out = {"voices": {}}
for p in sorted(glob.glob("/work/voices/*.bin")):
    x, sr = load(p)
    f0, n = f0_median(x, sr)
    out["voices"][os.path.basename(p)[:-4]] = {"f0": round(f0, 1), "voiced_frames": n, "seconds": round(len(x) / sr, 2)}

# music
x, sr = load("/work/music.mp3", 22050)
dur = len(x) / sr
hop = 512
frames = np.lib.stride_tricks.sliding_window_view(x, 2048)[::hop]
spec = np.abs(np.fft.rfft(frames * np.hanning(2048), axis=1))
flux = np.maximum(0, np.diff(np.log1p(spec), axis=0)).sum(axis=1)
flux = (flux - flux.mean()) / (flux.std() + 1e-9)
fps = sr / hop
# tempo from autocorrelation of the onset envelope, 70..180 bpm
ac = np.correlate(flux, flux, "full")[len(flux) - 1:]
lags = np.arange(len(ac))
bpm_lags = (fps * 60 / lags[1:])
mask = (bpm_lags > 70) & (bpm_lags < 180)
best = lags[1:][mask][np.argmax(ac[1:][mask])]
bpm = fps * 60 / best
# beat phase: pick the offset whose comb sums the most onset energy
period = fps * 60 / bpm
phases = np.arange(0, period, 0.5)
score = [flux[np.arange(ph, len(flux), period).astype(int)].sum() for ph in phases]
phase = phases[int(np.argmax(score))]
beats = [round((phase + i * period) / fps, 3) for i in range(int((len(flux) - phase) / period))]
# loudness per second, to find the build and the drop
rms = [float(np.sqrt((x[int(i * sr):int((i + 1) * sr)] ** 2).mean())) for i in range(int(dur))]
out["music"] = {"seconds": round(dur, 2), "bpm": round(float(bpm), 2), "first_beat": beats[0] if beats else 0,
                "beats": beats[:400], "rms_per_second": [round(r, 4) for r in rms]}
json.dump(out, open("/work/analysis.json", "w"), indent=1)
print(json.dumps(out["voices"], indent=1))
print("music", out["music"]["seconds"], "s, bpm", out["music"]["bpm"], "first beat", out["music"]["first_beat"])
print("rms/s:", " ".join(f"{r:.2f}" for r in rms))
