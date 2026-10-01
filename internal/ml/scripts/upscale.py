# /// script
# requires-python = ">=3.12"
# dependencies = ["torch", "spandrel==0.4.2", "safetensors", "huggingface_hub", "pillow", "numpy"]
# [tool.uv]
# exclude-newer = "2026-10-01T00:00:00Z"
# ///
"""upscale.py -- enlarge 4x with DAT (Dual Aggregation Transformer, x4).

    upscale.py --image in.png --out out.png --model REPO --revision SHA --file WEIGHTS

DAT is trained for fidelity to the original, so it sharpens the edges and
strokes that survive in the small image and invents no texture. The GAN
models (Real-ESRGAN x4plus and its relatives) paint fabric weave as satin
and redraw a painting's brushwork as crisp outlines, and came out further
from the original than plain Lanczos.

The image runs in TILE x TILE px tiles, each with PAD px of context on every
side that is cut away again, so memory stays near 2 GB at any size and no
seam shows. Alpha, which the model does not take, is enlarged with Lanczos.
"""

import argparse
import os
import sys
import time

import numpy as np
import torch
from huggingface_hub import hf_hub_download
from PIL import Image
from spandrel import ModelLoader

TILE = 256
PAD = 32


def main() -> None:
    ap = argparse.ArgumentParser()
    for k in ("--image", "--out", "--model", "--revision", "--file"):
        ap.add_argument(k, required=True)
    a = ap.parse_args()
    t0 = time.time()
    if torch.backends.mps.is_available():
        device = "mps"
    elif torch.cuda.is_available():
        device = "cuda"
    else:
        device = "cpu"
    loaded = ModelLoader().load_from_file(hf_hub_download(a.model, a.file, revision=a.revision))
    model, scale = loaded.model.to(device).eval(), loaded.scale
    mult = loaded.size_requirements.multiple_of or 1

    src = Image.open(a.image)
    alpha = src.getchannel("A") if "A" in src.getbands() else None
    rgb = np.asarray(src.convert("RGB"), dtype=np.float32) / 255
    h, w, _ = rgb.shape
    x = torch.from_numpy(rgb).permute(2, 0, 1)[None]
    out = torch.zeros(1, 3, h * scale, w * scale)
    tiles = 0
    for y0 in range(0, h, TILE):
        for x0 in range(0, w, TILE):
            y1, x1 = min(y0 + TILE, h), min(x0 + TILE, w)
            ya, xa = max(y0 - PAD, 0), max(x0 - PAD, 0)
            yb, xb = min(y1 + PAD, h), min(x1 + PAD, w)
            t = x[..., ya:yb, xa:xb]
            th, tw = t.shape[-2:]
            t = torch.nn.functional.pad(t, (0, (-tw) % mult, 0, (-th) % mult), mode="replicate")
            with torch.no_grad():
                o = model(t.to(device)).float().cpu()
            oy, ox = (y0 - ya) * scale, (x0 - xa) * scale
            out[..., y0 * scale : y1 * scale, x0 * scale : x1 * scale] = o[
                ..., oy : oy + (y1 - y0) * scale, ox : ox + (x1 - x0) * scale
            ]
            tiles += 1
    pixels = (out[0].clamp(0, 1).permute(1, 2, 0).numpy() * 255 + 0.5).astype(np.uint8)
    img = Image.fromarray(pixels)
    if alpha is not None:
        img.putalpha(alpha.resize(img.size, Image.Resampling.LANCZOS))
    img.save(a.out)
    print(f"upscale: {w}x{h} -> {img.width}x{img.height} on {device}, {tiles} tiles, {time.time() - t0:.1f}s")
    # torch's native teardown can abort after the output is written, turning
    # a good run into a non-zero exit. Leave without running it.
    sys.stdout.flush()
    sys.stderr.flush()
    os._exit(0)


if __name__ == "__main__":
    main()
