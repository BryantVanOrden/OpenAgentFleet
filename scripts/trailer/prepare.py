"""Copy the screenshots and logos the trailer uses into comp/a/.

The console shots are downsized to 2000 px wide JPEGs: they never show larger
than that, and full-size PNGs make every frame slower to render. The live
desktop keeps full resolution because the camera zooms into it.
"""
import os, shutil
from PIL import Image

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", ".."))
OUT = os.path.join(HERE, "comp", "a")
os.makedirs(OUT, exist_ok=True)
img = lambda *p: os.path.join(ROOT, *p)

shutil.copy(img("docs", "images", "desktop-live-dark.png"), os.path.join(OUT, "desktop.png"))
shutil.copy(img("docs", "images", "oaf_mascot_clean.png"), os.path.join(OUT, "oaf.png"))
for name in ("claude.svg", "codex.svg", "hermes.svg", "openclaw.svg"):
    shutil.copy(img("site", "assets", "agents", name), OUT)

for n in "org work ticket fleet engines chat session skills catalog launch alerts settings".split():
    im = Image.open(img("docs", "images", f"{n}-dark-amber.png")).convert("RGB")
    im.resize((2000, round(2000 * im.height / im.width)), Image.LANCZOS).save(os.path.join(OUT, f"{n}.jpg"), quality=92)
for n in ("theme-dark-purple", "theme-dark-green", "fleet-light-blue", "org-light-blue"):
    im = Image.open(img("docs", "images", f"{n}.png")).convert("RGB")
    im.resize((2000, round(2000 * im.height / im.width)), Image.LANCZOS).save(os.path.join(OUT, f"{n}.jpg"), quality=92)
for n in ("app-org-dark", "app-work-dark", "app-chat-dark"):
    Image.open(img("docs", "images", f"{n}.png")).convert("RGB").save(os.path.join(OUT, f"{n}.jpg"), quality=93)
print("assets in", OUT)
