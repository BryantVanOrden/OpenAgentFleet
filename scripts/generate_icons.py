import os
from PIL import Image

root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
src_png = r"C:\Users\borden\Downloads\OAF Icon.png"
img = Image.open(src_png).convert("RGBA")

# Save master transparent mascot image to docs/images/oaf_mascot.png
os.makedirs(os.path.join(root, "docs", "images"), exist_ok=True)
img.save(os.path.join(root, "docs", "images", "oaf_mascot.png"), "PNG")

# Crop tightly to the content bounding box
bbox = img.getbbox()
if bbox:
    cropped = img.crop(bbox)
else:
    cropped = img

# Create square canvas with small padding around the mascot
max_dim = max(cropped.width, cropped.height)
pad = int(max_dim * 0.08) # 8% padding
canvas_dim = max_dim + (pad * 2)
sq = Image.new("RGBA", (canvas_dim, canvas_dim), (0, 0, 0, 0))
offset_x = (canvas_dim - cropped.width) // 2
offset_y = (canvas_dim - cropped.height) // 2
sq.paste(cropped, (offset_x, offset_y), cropped)
sq.save(os.path.join(root, "docs", "images", "oaf_mascot_square.png"), "PNG")

def save_resized(img, path, size):
    full_path = os.path.normpath(os.path.join(root, path))
    os.makedirs(os.path.dirname(full_path), exist_ok=True)
    resized = img.resize(size, Image.Resampling.LANCZOS)
    resized.save(full_path, "PNG")

# 1. Branding
save_resized(sq, "mobile/assets/branding/icon.png", (1024, 1024))
save_resized(sq, "mobile/assets/branding/icon_foreground.png", (1024, 1024))
save_resized(sq, "mobile/assets/branding/splash.png", (1024, 1024))

# 2. Web icons
save_resized(sq, "mobile/web/favicon.png", (32, 32))
save_resized(sq, "mobile/web/icons/Icon-192.png", (192, 192))
save_resized(sq, "mobile/web/icons/Icon-512.png", (512, 512))
save_resized(sq, "mobile/web/icons/Icon-maskable-192.png", (192, 192))
save_resized(sq, "mobile/web/icons/Icon-maskable-512.png", (512, 512))

# 3. Admin public
save_resized(sq, "admin/public/favicon.png", (32, 32))
ico_path = os.path.normpath(os.path.join(root, "admin/public/favicon.ico"))
os.makedirs(os.path.dirname(ico_path), exist_ok=True)
sq.save(ico_path, format="ICO", sizes=[(16,16), (32,32), (48,48), (64,64)])

# 4. Linux desktop
save_resized(sq, "mobile/linux/packaging/agentfleet.png", (512, 512))

# 5. Windows .ico
win_ico = os.path.normpath(os.path.join(root, "mobile/windows/runner/resources/app_icon.ico"))
os.makedirs(os.path.dirname(win_ico), exist_ok=True)
sq.save(win_ico, format="ICO", sizes=[(16,16), (32,32), (48,48), (64,64), (128,128), (256,256)])

# 6. macOS AppIcon
mac_sizes = [16, 32, 64, 128, 256, 512, 1024]
for s in mac_sizes:
    save_resized(sq, f"mobile/macos/Runner/Assets.xcassets/AppIcon.appiconset/app_icon_{s}.png", (s, s))

# 7. iOS AppIcon
ios_dir = os.path.normpath(os.path.join(root, "mobile/ios/Runner/Assets.xcassets/AppIcon.appiconset"))
if os.path.exists(ios_dir):
    for fname in os.listdir(ios_dir):
        if fname.endswith(".png"):
            fpath = os.path.join(ios_dir, fname)
            with Image.open(fpath) as current:
                cur_sz = current.size
            sq.resize(cur_sz, Image.Resampling.LANCZOS).save(fpath, "PNG")

# 8. Android mipmaps and drawables
android_mipmaps = {
    "mdpi": 48,
    "hdpi": 72,
    "xhdpi": 96,
    "xxhdpi": 144,
    "xxxhdpi": 192,
}
for density, sz in android_mipmaps.items():
    save_resized(sq, f"mobile/android/app/src/main/res/mipmap-{density}/ic_launcher.png", (sz, sz))
    save_resized(sq, f"mobile/android/app/src/main/res/drawable-{density}/ic_launcher_foreground.png", (int(sz * 1.5), int(sz * 1.5)))
    save_resized(sq, f"mobile/android/app/src/main/res/drawable-{density}/splash.png", (sz * 2, sz * 2))
    save_resized(sq, f"mobile/android/app/src/main/res/drawable-{density}/android12splash.png", (sz * 2, sz * 2))
    save_resized(sq, f"mobile/android/app/src/main/res/drawable-night-{density}/android12splash.png", (sz * 2, sz * 2))

print("All icons successfully generated and saved!")
