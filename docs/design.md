---
type: architecture
links:
  - rel: see-also
    to: docs/roadmap.md
    note: the order the pieces below get built in
---

# imgkit — design

The architecture imgkit is being built to; [roadmap.md](roadmap.md) tracks
what is built and what comes next.

## Shape

One Go binary, `go install`-able. Each subcommand is one generic operation;
nothing project-specific (crop boxes, paths, colours, thresholds tuned to one
image) lives here. A project keeps a short recipe that supplies those and
calls imgkit. Plain resize, crop and encode stay as `magick` calls in the
recipe, because wrapping them would add nothing.

```
main.go              dispatch
internal/<op>/       one package per command (cutout, inpaint, infill, upscale,
                     grade, render, press, diff, fonts, qr, doctor)
internal/cli/        flag, exit-code and error conventions every command shares
internal/engine/     the one way an external tool is run
internal/enginetest/ sh stubs that stand in for engines in tests
internal/pins/       the one table of engine versions
internal/pdf/        PDF page count, page boxes and text through poppler
internal/policy/     the imgkit.toml synthesis policy
internal/raster/     in-process pixel work: load, measure, small PNG edits
internal/ml/         embedded single-file Python scripts (uv run --script)
internal/vision/     embedded Swift helper for Apple Vision (macOS)
quality/             the test images, their licences, and cases.toml
```

## Engines

`internal/engine` runs every external tool with the same rules:

- a timeout on every call;
- success means the output file exists and is non-empty, never the exit code
  alone, because Chrome exits 0 on failure and prints noise on success;
- a per-engine stderr policy: fatal for Ghostscript, where stderr is the only
  sign it dropped an image;
- per-call exit-code handling (`engine.Cmd.OKExit`) for tools whose non-zero
  exit is not an error.

`internal/pins` is the only place an engine version is written down. `doctor`
reads it, and so does every "not installed" error, which prints the exact
install command.

`IMGKIT_ENGINE_<NAME>` points imgkit at a specific executable for an engine,
ahead of the data directory and `PATH`; tests use it to stand in sh stubs.

| Engine | How it is pinned |
|---|---|
| ImageMagick 7 | minimum version |
| chrome-headless-shell | exact version and sha256, installed by `doctor --install` under `$XDG_DATA_HOME/imgkit/` |
| Real-ESRGAN (ncnn-vulkan) | exact version and sha256, installed by `doctor --install` under `$XDG_DATA_HOME/imgkit/` |
| Ghostscript, poppler, qpdf, qrencode | minimum version |
| Apple Vision | the OS; macOS 14 or later |
| Python ML (ViTMatte, BiRefNet, LaMa, grade fit) | PEP 723 header with `exclude-newer`, and a committed `uv lock --script` lockfile run with `--locked`; Hugging Face model revision pinned by commit |

Python runs only for the ML steps, where no Go, Rust or shell tool of
comparable quality exists. `uv` is the only Python tool a user installs.

## Synthesis policy

`inpaint`, `infill` and `upscale` create pixels the camera or artist never
made. imgkit looks for `imgkit.toml` in the working directory and each parent;
if the nearest one sets `synthesis = "forbid"`, those commands exit with an
error naming the file. Everything else only computes an alpha channel, moves
colours, or renders what it is given.

## Operations

- **cutout** — a coarse mask (`--coarse vision`, the default on macOS, or
  `birefnet`), then ViTMatte refinement from a trimap, run in 1024 px tiles
  overlapping by 128 px so memory stays near 1 GB at any size. Then the
  foreground colour is estimated so soft edges lose the background's tint.
  An optional despill pulls a named contaminating hue toward the local clean
  colour. `--height` sets the working size, and it must never be smaller than
  the largest size the cut-out will be drawn at.
- **render** — refuses a PNG beyond the largest size verified whole on the
  pinned engine (`render.maxSide`), since Chrome has cut large screenshots
  short with exit 0; checks the PNG is exactly size × scale. It warns when a
  PDF page size is not a multiple of 8 CSS px, which Chrome's PDF backend
  rounds to, and fails when the PDF's page box does not match `--size` (no
  matching `@page` prints Letter). It deletes stale outputs first, stamps the
  PNG's pHYs at 96 × scale DPI, and fails if the page text contains a
  `--fail-if` string: dumped DOM for PNG, `pdftotext` for PDF.
- **press** — Ghostscript pdfwrite with outlined text, CMYK conversion to a
  supplied ICC, fixed media from the input's page box, and images downsampled
  only above `--max-ppi`. It then checks the result has no fonts, no RGB, and
  unchanged page boxes, and compares a soft proof against the source by RMSE
  over the page and any `--proof-region`.
- **grade** — `fit` registers the image to the reference (SIFT + RANSAC),
  fits an RGB→RGB thin-plate map on opaque, eroded pixels, and bakes a 16-bit
  HALD CLUT. `apply` runs the CLUT through ImageMagick and restores alpha,
  which `-hald-clut` drops.
- **diff** — crops both images to their overlap, searches a vertical shift,
  and scores the share of pixels outside a per-channel tolerance.
- **infill, inpaint, upscale, fonts, qr** — as the README table describes.

## Platforms and errors

macOS is primary. On Linux, everything works except `--coarse vision`, which
exits with a message pointing at `birefnet`. No command depends on GNU-only
flags. An operation that cannot do the job fails with the measured reason
("no foreground found", "render would be 61.7 MP"), and never leaves a
plausible-looking wrong file behind.

## Quality

`go test ./...` covers everything that needs no model or browser, standing
sh stubs in for engines through `IMGKIT_ENGINE_<NAME>`. `just quality` runs
every operation over `quality/`: openly licensed or synthetic test images,
each asset registered in `quality/assets.toml` with its source and licence,
each case in `quality/cases.toml` (fields: `quality.Case`) with its metric and
threshold (fringe pixels, see-through pixels inside the subject, seam
visibility, a refused oversized render). It needs the models and runs
locally, not in CI.
