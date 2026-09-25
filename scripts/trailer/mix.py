"""Music edit + narration + synthesized sound design, mixed and loudness-normalised."""
import json, subprocess, sys
import numpy as np
from scipy.signal import butter, sosfilt, fftconvolve
from scipy.io import wavfile

SR = 48000
cut = sys.argv[1]
TL = json.load(open("/work/comp/timeline.json"))[cut]
N = int(TL["duration"] * SR) + SR
R = np.random.default_rng(7)


def decode(path):
    raw = subprocess.run(["ffmpeg", "-v", "error", "-i", path, "-ac", "2", "-ar", str(SR), "-f", "f32le", "-"],
                         capture_output=True, check=True).stdout
    return np.frombuffer(raw, dtype=np.float32).reshape(-1, 2).copy()


def env_exp(n, tau):
    return np.exp(-np.arange(n) / (tau * SR))


def bp(x, lo, hi, order=2):
    return sosfilt(butter(order, [lo, hi], btype="band", fs=SR, output="sos"), x)


def lp(x, f, order=2):
    return sosfilt(butter(order, f, btype="low", fs=SR, output="sos"), x)


def hp(x, f, order=2):
    return sosfilt(butter(order, f, btype="high", fs=SR, output="sos"), x)


def sweep_filter(x, f0, f1, kind="band", q=1.2, block=256):
    """Noise through a filter whose centre glides from f0 to f1 (log)."""
    out = np.zeros_like(x)
    n = len(x)
    zi = None
    for i in range(0, n, block):
        p = i / max(1, n - 1)
        fc = f0 * (f1 / f0) ** p
        if kind == "band":
            lo, hi = fc / (1 + 1 / q), min(fc * (1 + 1 / q), SR / 2 - 100)
            sos = butter(2, [lo, hi], btype="band", fs=SR, output="sos")
        else:
            sos = butter(2, min(fc, SR / 2 - 100), btype=kind, fs=SR, output="sos")
        if zi is None or zi.shape[0] != sos.shape[0]:
            zi = np.zeros((sos.shape[0], 2))
        out[i:i + block], zi = sosfilt(sos, x[i:i + block], zi=zi)
    return out


def stereo(m, pan=0.0):
    l, r = np.cos((pan + 1) * np.pi / 4), np.sin((pan + 1) * np.pi / 4)
    return np.stack([m * l * 1.41, m * r * 1.41], axis=1)


IRS = {}


def reverb(st, wet=0.25, length=2.2):
    if length not in IRS:
        n = int(length * SR)
        e = env_exp(n, length / 6)
        ir = np.stack([hp(R.standard_normal(n), 200) * e, hp(R.standard_normal(n), 200) * e], axis=1)
        IRS[length] = ir / np.sqrt((ir ** 2).sum(axis=0))
    ir = IRS[length]
    out = np.stack([fftconvolve(st[:, c], ir[:, c]) for c in range(2)], axis=1) * wet
    out[:len(st)] += st * (1 - wet * 0.5)
    return out


# ------------------------------------------------------------------ sounds
def whoosh(d=0.45, up=True):
    n = int(d * SR)
    x = R.standard_normal(n)
    y = sweep_filter(x, 300 if up else 4000, 4500 if up else 300, "band", q=1.5)
    t = np.linspace(0, 1, n)
    e = (t ** 2.2) * (1 - t) ** 0.35 * 3.2
    m = y * e
    pan = np.linspace(-0.7, 0.7, n)
    st = np.stack([m * np.cos((pan + 1) * np.pi / 4), m * np.sin((pan + 1) * np.pi / 4)], axis=1) * 1.4
    return st / (np.abs(st).max() + 1e-9) * 0.5


def riser(d):
    n = int(d * SR)
    t = np.arange(n) / SR
    p = t / d
    x = sweep_filter(R.standard_normal(n), 400, 9000, "band", q=0.9)
    f = 180 * (2200 / 180) ** (p ** 1.4)
    ph = 2 * np.pi * np.cumsum(f) / SR
    tone = np.sin(ph) * 0.35 + np.sin(ph * 1.5) * 0.18 + np.sin(ph * 0.5) * 0.25
    trem = 1 + 0.35 * np.sin(2 * np.pi * (4 + 18 * p ** 2) * t)
    e = p ** 2.6
    m = (x * 0.9 + tone * 0.6) * e * trem
    m[-int(0.012 * SR):] *= np.linspace(1, 0, int(0.012 * SR))
    st = stereo(m) + np.stack([np.roll(m, 180), m], axis=1) * 0.25
    return st / (np.abs(st).max() + 1e-9) * 0.55


def boom(big=1.0):
    n = int(2.4 * SR)
    t = np.arange(n) / SR
    f = 32 + 48 * np.exp(-t * 5)
    sub = np.sin(2 * np.pi * np.cumsum(f) / SR) * env_exp(n, 0.55)
    thump = lp(R.standard_normal(n), 180) * env_exp(n, 0.07) * 3.0
    crack = hp(R.standard_normal(n), 1500) * env_exp(n, 0.02) * 0.9
    m = sub * 1.0 + thump + crack
    st = reverb(stereo(m), wet=0.35 * big, length=2.6)
    return st / (np.abs(st).max() + 1e-9) * 0.95


def impact():
    n = int(0.9 * SR)
    t = np.arange(n) / SR
    f = 55 + 90 * np.exp(-t * 22)
    body = np.sin(2 * np.pi * np.cumsum(f) / SR) * env_exp(n, 0.16)
    snap = bp(R.standard_normal(n), 900, 6000) * env_exp(n, 0.035) * 1.4
    st = reverb(stereo(body + snap), wet=0.3, length=1.4)
    return st / (np.abs(st).max() + 1e-9) * 0.8


def glitch(d=0.4):
    n = int(d * SR)
    out = np.zeros(n)
    i = 0
    while i < n:
        L = int(R.uniform(0.015, 0.05) * SR)
        f = R.choice([220, 440, 880, 1320, 1760, 3000])
        t = np.arange(min(L, n - i)) / SR
        sq = np.sign(np.sin(2 * np.pi * f * t))
        nz = np.round(R.standard_normal(len(t)) * 3) / 3
        seg = (sq * 0.5 + nz * 0.5) * (R.uniform() > 0.25)
        out[i:i + len(t)] = seg
        i += L
    out = np.round(out * 6) / 6
    return stereo(hp(out, 150), R.uniform(-0.4, 0.4)) * 0.3


def typing(d, density=1.0):
    n = int(d * SR)
    out = np.zeros(n + SR // 10)
    t = 0.0
    while t < d:
        k = int(t * SR)
        L = int(0.012 * SR)
        c = hp(R.standard_normal(L), 2500) * env_exp(L, 0.0025) * R.uniform(0.5, 1.0)
        out[k:k + L] += c
        t += R.uniform(0.045, 0.09) / density
    return stereo(out, 0.15) * 0.35


def mouse_click():
    n = int(0.12 * SR)
    out = np.zeros(n)
    for off, a in ((0, 1.0), (int(0.055 * SR), 0.6)):
        L = int(0.008 * SR)
        out[off:off + L] += bp(R.standard_normal(L), 1800, 7000) * env_exp(L, 0.0015) * a
    return stereo(out, 0.2) * 0.5


def tick():
    n = int(0.12 * SR)
    t = np.arange(n) / SR
    m = np.sin(2 * np.pi * 1900 * t) * env_exp(n, 0.018) + hp(R.standard_normal(n), 3000) * env_exp(n, 0.003) * 0.6
    return stereo(m) * 0.3


def blip():
    n = int(0.22 * SR)
    t = np.arange(n) / SR
    f = np.where(t < 0.08, 880, 1320)
    m = np.sin(2 * np.pi * np.cumsum(f) / SR) * (env_exp(n, 0.06) * 0.7 + 0.3 * (t < 0.2))
    m *= np.minimum(1, (0.22 - t) / 0.03)
    return stereo(m) * 0.22


def buzz():
    n = int(0.6 * SR)
    t = np.arange(n) / SR
    gate = ((t < 0.2) | ((t > 0.3) & (t < 0.5))).astype(float)
    gate = lp(gate, 60)
    m = np.tanh(np.sin(2 * np.pi * 165 * t) * 3) * gate * (0.8 + 0.2 * np.sin(2 * np.pi * 31 * t))
    return stereo(lp(m, 900)) * 0.45


def chime():
    n = int(1.4 * SR)
    t = np.arange(n) / SR
    m = np.zeros(n)
    for start, f in ((0.0, 1318.5), (0.11, 1975.5)):
        k = int(start * SR)
        tt = t[:n - k]
        bell = (np.sin(2 * np.pi * f * tt) + 0.3 * np.sin(2 * np.pi * f * 2.76 * tt) * np.exp(-tt * 9)) * np.exp(-tt * 3.2)
        m[k:] += bell
    st = reverb(stereo(m), wet=0.3, length=1.6)
    return st / (np.abs(st).max() + 1e-9) * 0.35


def clank():
    n = int(1.2 * SR)
    t = np.arange(n) / SR
    m = sum(np.sin(2 * np.pi * f * t) * np.exp(-t * d) * a for f, d, a in ((523, 9, .5), (1187, 14, .4), (1873, 20, .35), (2931, 28, .3), (4410, 40, .2)))
    m += hp(R.standard_normal(n), 2000) * env_exp(n, 0.006) * 1.2
    m += np.sin(2 * np.pi * 70 * t) * env_exp(n, 0.09) * 0.9
    st = reverb(stereo(m), wet=0.3, length=1.6)
    return st / (np.abs(st).max() + 1e-9) * 0.75


# ------------------------------------------------------------------ tracks
def place(track, st, t, gain=1.0):
    k = int(round(t * SR))
    if k < 0:
        st = st[-k:]
        k = 0
    e = min(len(track), k + len(st))
    track[k:e] += st[:e - k] * gain


music = np.zeros((N, 2))
src = decode("/work/music.mp3")
pos = 0
X = int(0.02 * SR)
for i, (a, b) in enumerate(TL["music"]):
    seg = src[int(a * SR):int(b * SR)].copy()
    if i > 0:
        seg[:X] *= np.linspace(0, 1, X)[:, None]
        music[pos - X:pos] *= np.linspace(1, 0, X)[:, None]
        pos -= 0
    L = min(len(seg), N - pos)
    music[pos:pos + L] += seg[:L]
    pos += L
fi = int(0.25 * SR)
music[:fi] *= np.linspace(0, 1, fi)[:, None]
fa, fb = TL["musicFade"]
tt = np.arange(N) / SR
music *= np.clip(1 - (tt - fa) / (fb - fa), 0, 1)[:, None] ** 1.5

vo = np.zeros((N, 2))
for key, t in TL["vo"]:
    sr, w = wavfile.read(f"/work/vo/final/{key}.wav")
    w = w.astype(np.float32) / (32768.0 if w.dtype == np.int16 else 1.0)
    if w.ndim == 1:
        w = np.stack([w, w], axis=1)
    place(vo, w, t)

sfx = np.zeros((N, 2))
cache = {}
for s in TL["scenes"]:
    if s["a"] > 0.05 and not s.get("nowhoosh"):
        place(sfx, whoosh(0.42), s["a"] - 0.40, 0.8)
    if s["type"] == "montage":
        for c in s["cuts"][1:]:
            place(sfx, whoosh(0.22), s["a"] + c - 0.2, 0.55)
    for k in s.get("cursor", []):
        if k.get("click"):
            place(sfx, mouse_click(), s["a"] + k["t"], 0.9)
for name, t, *rest in TL["sfx"]:
    g = rest[-1] if rest else 1.0
    if name == "riser":
        place(sfx, riser(rest[0]), t, rest[1] if len(rest) > 1 else 1.0)
    elif name == "type":
        place(sfx, typing(rest[0]), t, rest[1] if len(rest) > 1 else 1.0)
    elif name == "boom":
        place(sfx, boom(rest[0] if rest else 1.0), t, rest[0] if rest else 1.0)
    else:
        fn = {"impact": impact, "glitch": glitch, "tick": tick, "blip": blip, "buzz": buzz, "chime": chime, "clank": clank}[name]
        place(sfx, fn(), t, g)

# duck the music under the voice
m = np.abs(vo).max(axis=1)
env = np.zeros(N)
a_c, r_c = np.exp(-1 / (0.02 * SR)), np.exp(-1 / (0.28 * SR))
from scipy.signal import lfilter
# peak follower: fast attack via max of two filters
att = lfilter([1 - a_c], [1, -a_c], m)
rel = lfilter([1 - r_c], [1, -r_c], m)
env = np.maximum(att, rel)
k = np.clip(env / 0.04, 0, 1)[:, None]
mid = np.stack([bp(music[:, c], 180, 5000) for c in range(2)], axis=1)
rest = music - mid
music_d = mid * (1 - 0.83 * k) + rest * (1 - 0.45 * k)

mix = music_d * 0.55 + vo * 1.55 + sfx * 0.7
def db(x):
    return 20 * np.log10(np.sqrt((x ** 2).mean()) + 1e-9)
vob = np.stack([bp(vo[:, c], 300, 4000) for c in range(2)], axis=1)
mub = np.stack([bp(music_d[:, c], 300, 4000) for c in range(2)], axis=1)
for key, t in TL["vo"]:
    a, b = int((t + .3) * SR), int((t + 1.2) * SR)
    print(f"{key:9s} MIDBAND diff {db(vob[a:b] * 1.55) - db(mub[a:b] * .55):5.1f}")
    print(f"{key:9s} vo {db(vo[a:b] * 1.4):6.1f}  music {db(music_d[a:b] * .55):6.1f}  sfx {db(sfx[a:b] * .7):6.1f}  diff {db(vo[a:b] * 1.4) - db(music_d[a:b] * .55):5.1f}")
mix = mix[:int(TL["duration"] * SR)]
peak = np.abs(mix).max()
mix = mix / peak * 0.89
wavfile.write(f"/work/premix_{cut}.wav", SR, mix.astype(np.float32))

# two-pass loudness normalisation to -14 LUFS
meas = subprocess.run(["ffmpeg", "-hide_banner", "-i", f"/work/premix_{cut}.wav", "-af", "loudnorm=I=-14:TP=-1.2:LRA=11:print_format=json", "-f", "null", "-"],
                      capture_output=True, text=True).stderr
j = json.loads(meas[meas.rindex("{"):meas.rindex("}") + 1])
af = (f"loudnorm=I=-14:TP=-1.2:LRA=11:measured_I={j['input_i']}:measured_TP={j['input_tp']}:measured_LRA={j['input_lra']}"
      f":measured_thresh={j['input_thresh']}:offset={j['target_offset']}:linear=true,aresample=48000")
subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", f"/work/premix_{cut}.wav", "-af", af, "-c:a", "pcm_s16le", f"/work/mix_{cut}.wav"], check=True)
chk = subprocess.run(["ffmpeg", "-hide_banner", "-i", f"/work/mix_{cut}.wav", "-af", "loudnorm=print_format=summary", "-f", "null", "-"], capture_output=True, text=True).stderr
print(cut, "input", j["input_i"], "LUFS ->", [l for l in chk.splitlines() if "Input Integrated" in l or "Input True Peak" in l])
