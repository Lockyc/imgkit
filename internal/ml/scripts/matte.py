# /// script
# requires-python = ">=3.12"
# dependencies = [
#   "torch",
#   "torchvision",
#   "transformers",
#   "pillow",
#   "numpy",
#   "opencv-python-headless",
#   "pymatting",
# ]
# [tool.uv]
# exclude-newer = "2026-10-01T00:00:00Z"
# ///
"""matte.py -- refine a coarse mask into a hair-level alpha matte.

    matte.py --image frame.png --coarse mask.png --out out.png --model REPO --revision SHA
             --context-width W --context-height H --band-in D --band-out D
             --tile PX --overlap PX
             [--despill-hue 70:100 --despill-hue-end 200 --despill-chroma 6:40
              --despill-lmax 88 --despill-clean 15:65]

1. context: a coarse mask swallows background seen through hair, sometimes
   deep inside its silhouette, so the band must be wide: the mask eroded by
   height/band-in is sure foreground, and beyond it dilated by
   height/band-out is sure background. ViTMatte solves that band in one
   whole-frame pass with the frame scaled to --context-width x
   --context-height, where it sees the whole subject.
   On full-resolution tiles the same wide band turns solid parts that
   resemble the background (a grey sleeve over pale bark) see-through.
2. trimap: the context alpha, thresholded at 0.5, replaces the coarse mask.
   It eroded by height/REFINE_IN_DIV is sure foreground, beyond it dilated by
   height/band-out is sure background, and the context's soft pixels, grown
   by height/SOFT_DIV, are unknown. REFINE_IN_DIV and SOFT_DIV are fixed,
   not flags: they only frame the context's own edge for the full-resolution
   pass, and how deep a coarse mask is wrong is the subject-dependent choice,
   which --band-in makes in the context pass.
3. alpha: ViTMatte predicts alpha over the unknown band at full resolution,
   on tile-px tiles overlapping by overlap px, blended with a linear ramp,
   and only on tiles with unknown pixels. The network's global attention
   scales with the square of the token count: a whole 2400 px frame needs
   about 27 GB. A tile needs a few hundred MB, and the result stays within
   1% RMSE of the whole frame with no seam. --tile 0 runs the whole frame.
4. colour: pymatting's multilevel foreground estimation un-mixes each
   partly transparent pixel toward its foreground colour, removing the
   background tint the camera recorded through the hair. Alpha is untouched.
5. despill (only with --despill-hue): where thin strands lie over the
   background, the matte rightly keeps the pixel opaque and its colour is a
   mix. Near the silhouette, pixels whose Lab hue has drifted from the
   subject's toward the background's, at modest chroma and below --lmax,
   have their chroma pulled toward the alpha-weighted local average of clean
   subject pixels (hue within --despill-clean). Lightness and alpha are
   untouched.

Every step segments or un-mixes pixels the camera captured. None invents one.
"""

import argparse
import os
import sys
import time

import cv2
import numpy as np
import torch
from imgkit_device import pick
from PIL import Image
from pymatting import estimate_foreground_ml
from transformers import VitMatteForImageMatting, VitMatteImageProcessor

REFINE_IN_DIV = 60
SOFT_DIV = 50
DESPILL_OUT_DIV = 40
DESPILL_IN_DIV = 25
DESPILL_REF_DIV = 120


def pair(s: str) -> tuple[float, float]:
    a, b = s.split(":")
    return float(a), float(b)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--image", required=True)
    ap.add_argument("--coarse", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--model", required=True)
    ap.add_argument("--revision", required=True)
    ap.add_argument("--context-width", type=int, required=True)
    ap.add_argument("--context-height", type=int, required=True)
    ap.add_argument("--band-in", type=int, required=True)
    ap.add_argument("--band-out", type=int, required=True)
    ap.add_argument("--tile", type=int, required=True)
    ap.add_argument("--overlap", type=int, required=True)
    ap.add_argument("--despill-hue", type=pair)
    ap.add_argument("--despill-hue-end", type=float)
    ap.add_argument("--despill-chroma", type=pair)
    ap.add_argument("--despill-lmax", type=float)
    ap.add_argument("--despill-clean", type=pair)
    a = ap.parse_args()
    spill = [a.despill_hue, a.despill_hue_end, a.despill_chroma, a.despill_lmax, a.despill_clean]
    if any(v is not None for v in spill) and any(v is None for v in spill):
        ap.error("despill needs all five of --despill-hue, --despill-hue-end, --despill-chroma, --despill-lmax and --despill-clean")

    img = Image.open(a.image).convert("RGB")
    coarse = cv2.imread(a.coarse, cv2.IMREAD_GRAYSCALE)
    if coarse is None:
        sys.exit(f"matte: cannot read {a.coarse}")
    if coarse.shape != (img.height, img.width):
        sys.exit(f"matte: mask {coarse.shape[::-1]} does not match image {img.size}")

    h = img.height
    device = pick()
    processor = VitMatteImageProcessor.from_pretrained(a.model, revision=a.revision)
    model = VitMatteForImageMatting.from_pretrained(a.model, revision=a.revision).to(device).eval()
    t0 = time.time()

    size = (a.context_width, a.context_height)
    # Both are area averages, so the context frame and its trimap line up.
    small = img.resize(size, Image.BOX) if size != img.size else img
    coarse_small = cv2.resize(coarse, small.size, interpolation=cv2.INTER_AREA)
    context, _ = matte_tiled(small, trimap(binary(coarse_small), small.height, a.band_in, a.band_out), processor, model, device, 0, 0)
    context = cv2.resize(context, img.size, interpolation=cv2.INTER_LINEAR)

    mask = binary((context * 255).round().astype(np.uint8))
    tri = trimap(mask, h, REFINE_IN_DIV, a.band_out)
    soft = ((context > 0.02) & (context < 0.98)).astype(np.uint8)
    tri[cv2.dilate(soft, disc(max(1, h // SOFT_DIV))) > 0] = 128
    alpha, tiles = matte_tiled(img, tri, processor, model, device, a.tile, a.overlap)

    rgb = np.asarray(img).astype(np.float64) / 255.0
    fg = np.clip(estimate_foreground_ml(rgb, alpha), 0.0, 1.0)
    fg8 = (fg * 255).round().astype(np.uint8)
    touched = 0
    if a.despill_hue:
        fg8, touched = despill(fg8, alpha, mask, h, a)

    rgba = np.dstack([fg8, (alpha * 255).round().astype(np.uint8)])
    Image.fromarray(rgba, "RGBA").save(a.out)
    print(f"matte: {img.width}x{img.height} on {device}, {tiles} tiles, despilled {touched} px, {time.time() - t0:.1f}s")
    # torch and its native runtimes can abort in interpreter teardown after
    # the output is written, turning a good run into a non-zero exit. Leave
    # without running it.
    sys.stdout.flush()
    sys.stderr.flush()
    os._exit(0)


def disc(r: int) -> np.ndarray:
    return cv2.getStructuringElement(cv2.MORPH_ELLIPSE, (2 * r + 1, 2 * r + 1))


def binary(mask8: np.ndarray) -> np.ndarray:
    return (mask8 > 127).astype(np.uint8) * 255


def trimap(mask: np.ndarray, h: int, band_in: int, band_out: int) -> np.ndarray:
    """mask eroded by h/band_in is sure foreground (255), beyond it dilated by
    h/band_out is sure background (0), and the band between is unknown (128)."""
    t = np.full(mask.shape, 128, np.uint8)
    t[cv2.erode(mask, disc(max(1, h // band_in))) > 0] = 255
    t[cv2.dilate(mask, disc(max(1, h // band_out))) == 0] = 0
    return t


def despill(rgb8, alpha, mask, h, a):
    zone = (cv2.dilate(mask, disc(h // DESPILL_OUT_DIV)) > 0) & (cv2.erode(mask, disc(h // DESPILL_IN_DIV)) == 0)
    lab = cv2.cvtColor(rgb8, cv2.COLOR_RGB2LAB).astype(np.float32)
    L, A, B = lab[..., 0] * 100.0 / 255.0, lab[..., 1] - 128.0, lab[..., 2] - 128.0
    hue = np.degrees(np.arctan2(B, A)) % 360.0
    chroma = np.hypot(A, B)
    hue_from, hue_to = a.despill_hue
    cmin, cmax = a.despill_chroma
    w = np.clip((hue - hue_from) / (hue_to - hue_from), 0.0, 1.0)
    w *= (hue < a.despill_hue_end) & (chroma > cmin) & (chroma < cmax)
    w *= (L < a.despill_lmax) & zone & (alpha > 0.02)
    lo, hi = a.despill_clean
    clean = ((hue > lo) & (hue < hi) & (alpha > 0.5) & (chroma > 8.0)).astype(np.float32) * alpha.astype(np.float32)
    sigma = h / DESPILL_REF_DIV
    den = cv2.GaussianBlur(clean, (0, 0), sigma) + 1e-6
    ref_a = cv2.GaussianBlur(A * clean, (0, 0), sigma) / den
    ref_b = cv2.GaussianBlur(B * clean, (0, 0), sigma) / den
    lab2 = np.dstack([L * 255.0 / 100.0, A * (1 - w) + ref_a * w + 128.0, B * (1 - w) + ref_b * w + 128.0])
    out = cv2.cvtColor(np.clip(lab2, 0, 255).astype(np.uint8), cv2.COLOR_LAB2RGB)
    return out, int((w > 0.05).sum())


def tile_starts(length: int, tile: int, overlap: int) -> list[int]:
    if tile == 0 or length <= tile:
        return [0]
    starts = list(range(0, length - tile, tile - overlap))
    starts.append(length - tile)
    return starts


def ramp_window(height: int, width: int, overlap: int) -> np.ndarray:
    def ramp(n: int) -> np.ndarray:
        w = np.ones(n, np.float32)
        r = min(overlap, n // 2)
        if r:
            edge = (np.arange(r, dtype=np.float32) + 1.0) / (r + 1.0)
            w[:r] = edge
            w[n - r :] = edge[::-1]
        return w
    return np.outer(ramp(height), ramp(width))


def matte_tiled(img, trimap, processor, model, device, tile, overlap):
    H, W = trimap.shape
    img_np = np.asarray(img)
    acc = np.zeros((H, W), np.float32)
    weight = np.zeros((H, W), np.float32)
    ran = 0
    size_h, size_w = (H, W) if tile == 0 else (tile, tile)
    for y in tile_starts(H, tile, overlap):
        for x in tile_starts(W, tile, overlap):
            t = trimap[y : y + size_h, x : x + size_w]
            th, tw = t.shape
            win = ramp_window(th, tw, 0 if tile == 0 else overlap)
            if not (t == 128).any():
                acc[y : y + th, x : x + tw] += (t.astype(np.float32) / 255.0) * win
                weight[y : y + th, x : x + tw] += win
                continue
            # The processor pads to a multiple of 32 with zeros, which reads
            # as a dark strip of sure background and skews the alpha along
            # the frame's bottom and right edges. Mirror the content instead.
            ph, pw = -th % 32, -tw % 32
            ti = cv2.copyMakeBorder(np.ascontiguousarray(img_np[y : y + th, x : x + tw]), 0, ph, 0, pw, cv2.BORDER_REFLECT_101)
            tt = cv2.copyMakeBorder(np.ascontiguousarray(t), 0, ph, 0, pw, cv2.BORDER_REFLECT_101)
            inputs = processor(images=Image.fromarray(ti), trimaps=Image.fromarray(tt), return_tensors="pt").to(device)
            with torch.no_grad():
                al = model(**inputs).alphas[0, 0].float().cpu().numpy()[:th, :tw]
            acc[y : y + th, x : x + tw] += np.clip(al, 0.0, 1.0) * win
            weight[y : y + th, x : x + tw] += win
            ran += 1
    return acc / np.maximum(weight, 1e-6), ran


if __name__ == "__main__":
    main()
