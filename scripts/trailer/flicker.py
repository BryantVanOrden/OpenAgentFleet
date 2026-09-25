import sys, os, glob, json
import numpy as np
from PIL import Image
d = sys.argv[1]
fs = sorted(glob.glob(os.path.join(d, "f*.jpg")))
def load(f):
    return np.asarray(Image.open(f).convert("L").resize((240, 135) if "frames_h" in d else (135, 240)), dtype=np.float32)
prev2 = load(fs[0]); prev = load(fs[1]); bad = []
for i in range(2, len(fs)):
    cur = load(fs[i])
    a = np.abs(prev - prev2).mean(); b = np.abs(cur - prev).mean(); c = np.abs(cur - prev2).mean()
    # frame i-1 is an outlier if it differs from both neighbours much more than they differ from each other
    if a > 4 and b > 4 and c < 0.5 * min(a, b):
        bad.append((i - 1, round(a, 1), round(b, 1), round(c, 1)))
    prev2, prev = prev, cur
print(len(bad), "outlier frames")
for x in bad: print(f"f{x[0]:05d} t={x[0]/30:6.2f}s  d_prev={x[1]} d_next={x[2]} d_skip={x[3]}")
