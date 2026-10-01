# /// script
# requires-python = ">=3.12"
# dependencies = ["torch", "spandrel==0.4.2", "safetensors", "huggingface_hub", "pillow", "numpy"]
# [tool.uv]
# exclude-newer = "2026-10-01T00:00:00Z"
# ///
"""upscale.py -- enlarge 4x with DAT (Dual Aggregation Transformer, x4).

    upscale.py --image frame.png --out out.png --model REPO --revision SHA --file WEIGHTS

The image is imgkit's normalised frame: oriented, sRGB, 8-bit RGBA. DAT is
trained for fidelity to the original, so it sharpens the edges and strokes
that survive in the small image and invents no texture.

The model runs on TILE x TILE px tiles, each with PAD px of context on every
side that is cut away again, and each tile is written straight into an
8-bit output array. Peak memory is that array and the PIL image made
from it (about 7 bytes per output pixel, 11 with alpha), the model and one
tile's activations: 2.3 GB for a 2000x3000 source enlarged to 8000x12000
on MPS. Alpha, which the model
does not take, is enlarged with Lanczos, and dropped when it is opaque
everywhere.
"""

import argparse
import os
import sys
import time

import numpy as np
import torch
from huggingface_hub import hf_hub_download
from imgkit_device import pick
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
    device = pick()
    loaded = ModelLoader().load_from_file(hf_hub_download(a.model, a.file, revision=a.revision))
    model, scale = loaded.model.to(device).eval(), loaded.scale
    mult = loaded.size_requirements.multiple_of or 1

    src = Image.open(a.image)
    alpha = src.getchannel("A") if "A" in src.getbands() else None
    if alpha is not None and alpha.getextrema() == (255, 255):
        alpha = None
    rgb = np.asarray(src.convert("RGB"), dtype=np.float32) / 255
    h, w, _ = rgb.shape
    x = torch.from_numpy(rgb).permute(2, 0, 1)[None]
    del rgb
    out = np.empty((h * scale, w * scale, 3), dtype=np.uint8)
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
                o = model(t.to(device))[0]
            oy, ox = (y0 - ya) * scale, (x0 - xa) * scale
            o = o[:, oy : oy + (y1 - y0) * scale, ox : ox + (x1 - x0) * scale]
            o = (o.float().clamp(0, 1) * 255 + 0.5).to(torch.uint8).permute(1, 2, 0).cpu().numpy()
            out[y0 * scale : y1 * scale, x0 * scale : x1 * scale] = o
            tiles += 1
    img = Image.fromarray(out)
    del out
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
