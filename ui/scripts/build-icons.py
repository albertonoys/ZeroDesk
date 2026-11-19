#!/usr/bin/env python3
"""Turn the generated icon artwork in design/icon-gen into web assets.

App icons: full-bleed square art -> 256 px WebP (the rounded tile is applied
in the browser with the shape fitted from a real macOS icon).
Trash: needs transparency; magenta (#FF00FF) backgrounds are keyed out.

Usage: python3 ui/scripts/build-icons.py   (from the zerodesk folder)
"""
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parents[2]
SRC = ROOT / "design" / "icon-gen"
OUT = ROOT / "ui" / "public" / "icons"
SIZE = 256

APP_ICONS = ["music", "apps", "settings", "terminal", "files", "raspberry", "about", "piano", "voicememos", "cardputer", "appstore"] + sorted(
    p.name[: -len("-light.png")] for p in SRC.glob("off-*-light.png")
)
# the trash prompt produced "light" = empty and "dark" = full
TRASH = {"trash-light": "trash-empty", "trash-dark": "trash-full"}


def square(im: Image.Image) -> Image.Image:
    w, h = im.size
    s = min(w, h)
    return im.crop(((w - s) // 2, (h - s) // 2, (w - s) // 2 + s, (h - s) // 2 + s))


def key_magenta(im: Image.Image) -> Image.Image:
    """Make near-magenta pixels transparent, with a soft edge."""
    im = im.convert("RGBA")
    px = im.load()
    w, h = im.size
    for y in range(h):
        for x in range(w):
            r, g, b, a = px[x, y]
            # distance from pure magenta
            d = abs(r - 255) + g + abs(b - 255)
            if d < 90:
                px[x, y] = (r, g, b, 0)
            elif d < 180:
                t = (d - 90) / 90
                # remove the magenta cast from edge pixels
                px[x, y] = (min(r, int(g + 40)), g, min(b, int(g + 40)), int(a * t))
    return im


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    for name in APP_ICONS:
        for mode in ("light", "dark"):
            src = SRC / f"{name}-{mode}.png"
            if not src.exists():
                print("missing", src.name)
                continue
            im = square(Image.open(src).convert("RGB")).resize((SIZE, SIZE), Image.LANCZOS)
            im.save(OUT / f"{name}-{mode}.webp", quality=92, method=6)
            print("ok", name, mode)
    for src_name, out_name in TRASH.items():
        src = SRC / f"{src_name}.png"
        if not src.exists():
            print("missing", src.name)
            continue
        im = Image.open(src)
        has_alpha = im.mode in ("RGBA", "LA") and im.getextrema()[3][0] < 250
        im = square(im.convert("RGBA"))
        if not has_alpha:
            im = key_magenta(im)
        box = im.getbbox()
        if box:
            # keep it square around the bin, with a small margin
            cx, cy = (box[0] + box[2]) / 2, (box[1] + box[3]) / 2
            half = max(box[2] - box[0], box[3] - box[1]) / 2 * 1.27
            im = im.crop((int(cx - half), int(cy - half), int(cx + half), int(cy + half)))
        im.resize((SIZE, SIZE), Image.LANCZOS).save(OUT / f"{out_name}.webp", quality=92, method=6)
        print("ok", out_name, "alpha" if has_alpha else "keyed")
    favicon()


def favicon() -> None:
    """Browser-tab icon: the Cardputer art masked to the macOS tile shape."""
    import re
    import subprocess
    import tempfile

    src = SRC / "cardputer-light.png"
    if not src.exists():
        return
    path = re.search(r"'(M[^']+)'", (ROOT / "ui" / "src" / "lib" / "icon-shape.ts").read_text()).group(1)
    with tempfile.TemporaryDirectory() as tmp:
        svg = Path(tmp) / "mask.svg"
        png = Path(tmp) / "mask.png"
        svg.write_text(
            f"<svg xmlns='http://www.w3.org/2000/svg' viewBox='9.88 9.88 80.24 80.24' width='512' height='512'>"
            f"<path d='{path}' fill='#000'/></svg>"
        )
        subprocess.run(["rsvg-convert", str(svg), "-o", str(png)], check=True)
        mask = Image.open(png).split()[3]
        art = square(Image.open(src).convert("RGBA")).resize((512, 512), Image.LANCZOS)
        art.putalpha(mask)
        art.resize((180, 180), Image.LANCZOS).save(ROOT / "ui" / "public" / "apple-touch-icon.png")
        art.resize((64, 64), Image.LANCZOS).save(ROOT / "ui" / "public" / "favicon.png")
    print("ok favicon")


if __name__ == "__main__":
    main()
