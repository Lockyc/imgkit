# /// script
# requires-python = ">=3.12"
# dependencies = ["rembg[cpu]==2.0.84", "pillow"]
# [tool.uv]
# exclude-newer = "2026-10-01T00:00:00Z"
# ///
"""birefnet.py -- the coarse foreground mask from rembg's birefnet-general.

    birefnet.py <frame.png> <mask.png>

birefnet-general, because on fur over similar-coloured brush strokes
birefnet-general-lite left body pixels semi-transparent, birefnet-massive
left the lower body see-through, isnet-general-use left fur see-through
and u2net dropped an arm. The model finds its edge at 1024 px, so this mask
is only the prior; matte.py solves the edge at full size.
"""

import os
import sys

from PIL import Image
from rembg import new_session, remove


def main() -> None:
    if len(sys.argv) != 3:
        sys.exit("usage: birefnet.py <frame.png> <mask.png>")
    img = Image.open(sys.argv[1]).convert("RGB")
    mask = remove(img, session=new_session("birefnet-general"), only_mask=True)
    mask.convert("L").save(sys.argv[2])
    # onnxruntime sometimes aborts in its C++ teardown after the work is done,
    # turning a good run into a non-zero exit. Leave without running it.
    sys.stdout.flush()
    sys.stderr.flush()
    os._exit(0)


if __name__ == "__main__":
    main()
