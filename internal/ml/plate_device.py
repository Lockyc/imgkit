"""plate_device.py -- the one rule for which device a torch model runs on.

$PLATE_DEVICE when set (cpu, mps or cuda); otherwise the first of mps, cuda
and cpu that this torch can use. The embedded scripts import pick() from
it, and plate runs it as a script inside a pinned tool's environment to
choose that tool's --device, so every model asks the same torch question.
"""

import os
import sys

import torch

DEVICES = ("cpu", "mps", "cuda")


def pick() -> str:
    d = os.environ.get("PLATE_DEVICE", "")
    if d:
        if d not in DEVICES:
            sys.exit(f"PLATE_DEVICE={d}: use one of {', '.join(DEVICES)}")
        return d
    if torch.backends.mps.is_available():
        return "mps"
    if torch.cuda.is_available():
        return "cuda"
    return "cpu"


if __name__ == "__main__":
    print(pick())
