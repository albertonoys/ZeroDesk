#!/usr/bin/env python3
"""Icons for the package and the Store listing, from design/icon-gen art.

packaging/build/zerodesk-cp0.png  100x100 launcher icon, rounded tile on transparency
store/icon.png                256x256 Store icon, full-bleed (the Store asks for no built-in corners)

Store screenshots (store/screenshots/*.png, 320x170) are rendered by the
agent's own drawing code:
  ZERODESK_PREVIEW_DIR=<dir> ZERODESK_CJK_FONT=<a CJK .ttf> go test -run TestRenderScreen ./agent
"""
from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[1]
ART = ROOT / "design" / "icon-gen" / "cardputer-light.png"


def square(im):
    w, h = im.size
    s = min(w, h)
    return im.crop(((w - s) // 2, (h - s) // 2, (w - s) // 2 + s, (h - s) // 2 + s))


def main():
    art = square(Image.open(ART).convert("RGBA"))
    (ROOT / "store").mkdir(exist_ok=True)
    art.resize((256, 256), Image.LANCZOS).convert("RGB").save(ROOT / "store" / "icon.png", optimize=True)

    big = 400  # draw the mask large, then downscale for smooth corners
    tile = art.resize((big, big), Image.LANCZOS)
    mask = Image.new("L", (big, big), 0)
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, big - 1, big - 1), radius=int(big * 0.225), fill=255)
    tile.putalpha(mask)
    out = ROOT / "packaging" / "build"
    out.mkdir(parents=True, exist_ok=True)
    tile.resize((100, 100), Image.LANCZOS).save(out / "zerodesk-cp0.png", optimize=True)
    print("icons ok")


if __name__ == "__main__":
    main()
