#!/usr/bin/env python3

from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

from PIL import Image


REPO_ROOT = Path(__file__).resolve().parents[2]
BRANDING_DIR = REPO_ROOT / "assets" / "branding"
OUT_DIR = BRANDING_DIR / "desktop"
SOURCE_SVG = BRANDING_DIR / "codex-remote-logo.svg"
TRAY_TEMPLATE_SVG = OUT_DIR / "codex-remote-tray-template.svg"
APP_ICONSET_DIR = OUT_DIR / "codex-remote-app.iconset"

APP_ICONSET_SIZES = {
    "icon_16x16.png": 16,
    "icon_16x16@2x.png": 32,
    "icon_32x32.png": 32,
    "icon_32x32@2x.png": 64,
    "icon_128x128.png": 128,
    "icon_128x128@2x.png": 256,
    "icon_256x256.png": 256,
    "icon_256x256@2x.png": 512,
    "icon_512x512.png": 512,
    "icon_512x512@2x.png": 1024,
}

TRAY_COLOR_SIZES = (16, 20, 24, 32, 40, 48, 64)
ICO_SIZES = [(size, size) for size in (16, 20, 24, 32, 40, 48, 64, 128, 256)]
TRAY_TEMPLATE_PNGS = {
    "codex-remote-tray-template.png": 18,
    "codex-remote-tray-template@2x.png": 36,
}


def ensure_magick() -> str:
    magick = shutil.which("magick")
    if not magick:
        raise SystemExit("ImageMagick `magick` is required to generate desktop icons.")
    return magick


def run(*args: str) -> None:
    subprocess.run(args, check=True)


def render_svg(magick: str, src: Path, dst: Path, size: int) -> None:
    run(
        magick,
        "-background",
        "none",
        "-density",
        "384",
        str(src),
        "-resize",
        f"{size}x{size}",
        str(dst),
    )


def resized(image: Image.Image, size: int) -> Image.Image:
    return image.resize((size, size), Image.Resampling.LANCZOS)


def main() -> int:
    magick = ensure_magick()

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    if APP_ICONSET_DIR.exists():
        shutil.rmtree(APP_ICONSET_DIR)
    APP_ICONSET_DIR.mkdir(parents=True)

    with tempfile.TemporaryDirectory() as temp_dir:
        master_png = Path(temp_dir) / "codex-remote-app-1024.png"
        render_svg(magick, SOURCE_SVG, master_png, 1024)
        master = Image.open(master_png).convert("RGBA")

        for filename, size in APP_ICONSET_SIZES.items():
            resized(master, size).save(APP_ICONSET_DIR / filename)

        master.save(OUT_DIR / "codex-remote-app.ico", format="ICO", sizes=ICO_SIZES)
        master.save(OUT_DIR / "codex-remote-app.icns", format="ICNS")

        for size in TRAY_COLOR_SIZES:
            resized(master, size).save(OUT_DIR / f"codex-remote-tray-color-{size}.png")

    for filename, size in TRAY_TEMPLATE_PNGS.items():
        render_svg(magick, TRAY_TEMPLATE_SVG, OUT_DIR / filename, size)

    return 0


if __name__ == "__main__":
    sys.exit(main())
