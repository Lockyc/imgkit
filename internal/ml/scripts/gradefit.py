# /// script
# requires-python = ">=3.12"
# dependencies = ["numpy", "opencv-python-headless", "pillow", "scipy"]
# [tool.uv]
# exclude-newer = "2026-10-01T00:00:00Z"
# ///
"""gradefit.py -- recover a reference's colour treatment of a subject as a HALD CLUT.

    gradefit.py --subject s.png --ref r.png --out hald.png [--ref-crop x,y,w,h]
                [--exclude x,y,w,h ...] [--level 8] [--samples 6000]
                [--smoothing 2.0] [--min-inliers 40] [--features 4000]

1. register the subject to the reference (SIFT, Lowe ratio 0.75, RANSAC
   similarity with a 2 px threshold), masked to the subject's opaque pixels;
2. blur both (sigma 3) so texture and residual misalignment drop out, and
   sample pixels opaque in the warped subject, eroded 15 px from its
   silhouette and outside every --exclude box (where the reference draws
   over the subject);
3. fit a smooth RGB->RGB thin-plate RBF (smoothing 2, degree 1) on a seeded
   sample and bake it into a 16-bit HALD CLUT.

A lookup moves colours, never pixels, and is fitted only on the subject.
"""

import argparse
import os
import sys

import cv2
import numpy as np
from PIL import Image
from scipy.interpolate import RBFInterpolator


def rect(s: str) -> tuple[int, int, int, int]:
    x, y, w, h = (int(v) for v in s.split(","))
    return x, y, w, h


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--subject", required=True)
    ap.add_argument("--ref", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--ref-crop", type=rect)
    ap.add_argument("--exclude", type=rect, action="append", default=[])
    ap.add_argument("--level", type=int, default=8)
    ap.add_argument("--samples", type=int, default=6000)
    ap.add_argument("--smoothing", type=float, default=2.0)
    ap.add_argument("--min-inliers", type=int, default=40)
    ap.add_argument("--features", type=int, default=4000)
    a = ap.parse_args()

    ours = np.asarray(Image.open(a.subject).convert("RGBA"))
    ref = np.asarray(Image.open(a.ref).convert("RGB"))
    if a.ref_crop:
        x, y, w, h = a.ref_crop
        ref = ref[y : y + h, x : x + w]
    h, w = ref.shape[:2]

    sift = cv2.SIFT_create(a.features)
    k1, d1 = sift.detectAndCompute(cv2.cvtColor(ours[..., :3], cv2.COLOR_RGB2GRAY), (ours[..., 3] > 200).astype(np.uint8))
    k2, d2 = sift.detectAndCompute(cv2.cvtColor(ref, cv2.COLOR_RGB2GRAY), None)
    if d1 is None or d2 is None:
        sys.exit("gradefit: no features found to register on")
    good = [p[0] for p in cv2.BFMatcher().knnMatch(d1, d2, k=2) if len(p) == 2 and p[0].distance < 0.75 * p[1].distance]
    if len(good) < 3:
        sys.exit(f"gradefit: registration failed: {len(good)} matches")
    p1 = np.float32([k1[g.queryIdx].pt for g in good])
    p2 = np.float32([k2[g.trainIdx].pt for g in good])
    M, inliers = cv2.estimateAffinePartial2D(p1, p2, method=cv2.RANSAC, ransacReprojThreshold=2.0)
    n_in = 0 if inliers is None else int(inliers.sum())
    if M is None or n_in < a.min_inliers:
        sys.exit(f"gradefit: registration failed: {len(good)} matches, {n_in} inliers (need {a.min_inliers})")
    warped = cv2.warpAffine(ours, M, (w, h), flags=cv2.INTER_LANCZOS4, borderValue=(0, 0, 0, 0))

    inner = cv2.erode((warped[..., 3] > 250).astype(np.uint8), np.ones((15, 15), np.uint8)).astype(bool)
    for ex, ey, ew, eh in a.exclude:
        inner[ey : ey + eh, ex : ex + ew] = False
    idx = np.flatnonzero(inner)
    if idx.size < 100:
        sys.exit(f"gradefit: only {idx.size} usable pixels after registration and exclusions")
    lo = lambda im: cv2.GaussianBlur(im.astype(np.float32), (0, 0), 3)
    src, dst = lo(warped[..., :3]), lo(ref)
    idx = np.random.default_rng(0).choice(idx, size=min(a.samples, idx.size), replace=False)
    X = src.reshape(-1, 3)[idx] / 255.0
    Y = dst.reshape(-1, 3)[idx] / 255.0

    rbf = RBFInterpolator(X, Y, kernel="thin_plate_spline", smoothing=a.smoothing, degree=1)
    g = np.linspace(0.0, 1.0, a.level**2)
    # ImageMagick's HALD layout: red fastest, then green, then blue.
    b, gr, r = np.meshgrid(g, g, g, indexing="ij")
    mapped = np.clip(rbf(np.stack([r.ravel(), gr.ravel(), b.ravel()], axis=1)), 0.0, 1.0)
    side = a.level**3
    hald = (mapped.reshape(side, side, 3) * 65535).round().astype(np.uint16)
    cv2.imwrite(a.out, cv2.cvtColor(hald, cv2.COLOR_RGB2BGR))
    print(f"gradefit: {len(good)} matches, {n_in} inliers, scale {np.hypot(M[0, 0], M[0, 1]):.4f}, fitted on {idx.size} pixels")
    # Native runtimes can abort in interpreter teardown after the output is
    # written; leave without running it so a good run keeps its exit 0.
    sys.stdout.flush()
    sys.stderr.flush()
    os._exit(0)


if __name__ == "__main__":
    main()
