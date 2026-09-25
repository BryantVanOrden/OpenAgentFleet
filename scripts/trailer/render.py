"""Render the trailer frame by frame: python render.py h|v [stills t1,t2,...] [workers]"""
import os, sys, threading, http.server, functools, time
from multiprocessing import Process
from playwright.sync_api import sync_playwright
import json
S = os.path.dirname(os.path.abspath(__file__))
COMP = os.path.join(S, "comp")
PORT = 5231

def serve():
    class Quiet(http.server.SimpleHTTPRequestHandler):
        def log_message(self, *a):
            pass
    h = functools.partial(Quiet, directory=COMP)
    class Srv(http.server.ThreadingHTTPServer):
        request_queue_size = 256
        daemon_threads = True
    Srv(("127.0.0.1", PORT), h).serve_forever()

def page_for(p, cut, tl):
    b = p.chromium.launch(args=["--disable-web-security", "--force-color-profile=srgb"])
    pg = b.new_page(viewport={"width": tl["w"], "height": tl["h"]}, device_scale_factor=1)
    pg.goto(f"http://127.0.0.1:{PORT}/index.html?cut={cut}")
    pg.wait_for_function("window.READY === true", timeout=180000)
    bad = pg.evaluate("[...document.images].filter(i => !i.complete || i.naturalWidth === 0).map(i => i.src)")
    if bad:
        raise SystemExit(f"{cut}: images failed to load: {bad}")
    return b, pg

def worker(cut, frames, outdir, fps):
    tl = json.load(open(os.path.join(COMP, "timeline.json")))[cut]
    with sync_playwright() as p:
        b, pg = page_for(p, cut, tl)
        for i in frames:
            pg.evaluate(f"render({i / fps})")
            pg.screenshot(path=os.path.join(outdir, f"f{i:05d}.jpg"), type="jpeg", quality=93)
        b.close()

if __name__ == "__main__":
    cut = sys.argv[1]
    threading.Thread(target=serve, daemon=True).start()
    tl = json.load(open(os.path.join(COMP, "timeline.json")))[cut]
    if len(sys.argv) > 2 and sys.argv[2] == "stills":
        out = os.path.join(S, "stills"); os.makedirs(out, exist_ok=True)
        with sync_playwright() as p:
            b, pg = page_for(p, cut, tl)
            for ts in sys.argv[3].split(","):
                pg.evaluate(f"render({float(ts)})")
                pg.screenshot(path=os.path.join(out, f"{cut}-{float(ts):06.2f}.jpg"), type="jpeg", quality=85)
            b.close()
        sys.exit(0)
    fps = 30
    n = int(round(tl["duration"] * fps))
    outdir = os.path.join(S, "frames_" + cut); os.makedirs(outdir, exist_ok=True)
    todo = [i for i in range(n) if not os.path.exists(os.path.join(outdir, f"f{i:05d}.jpg"))]
    W = int(sys.argv[2]) if len(sys.argv) > 2 else 4
    chunks = [todo[k::W] for k in range(W)]
    t0 = time.time()
    ps = [Process(target=worker, args=(cut, c, outdir, fps)) for c in chunks]
    # each process needs the server; start one per process
    for pr in ps: pr.start()
    for pr in ps: pr.join()
    print(f"{cut}: {len(todo)} frames in {time.time() - t0:.0f}s")
