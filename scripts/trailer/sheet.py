import sys, glob, os
from PIL import Image, ImageDraw
files = sys.argv[2:]; out = sys.argv[1]
ims = [Image.open(f) for f in files]
w, h = ims[0].size; sc = 960 / max(w, h) if w > h else 540 / h
tw, th = int(w * sc), int(h * sc); cols = 2 if w > h else 4
rows = (len(ims) + cols - 1) // cols
sheet = Image.new("RGB", (cols * tw + (cols - 1) * 6, rows * th + (rows - 1) * 6), "white")
for i, im in enumerate(ims):
    t = im.resize((tw, th)); d = ImageDraw.Draw(t); d.rectangle([0, 0, 110, 28], fill="black"); d.text((6, 6), os.path.basename(files[i])[2:-4], fill="yellow")
    sheet.paste(t, ((i % cols) * (tw + 6), (i // cols) * (th + 6)))
sheet.save(out, quality=88)
