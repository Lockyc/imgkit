# imgkit

[![CI](https://github.com/lockyc/imgkit/actions/workflows/ci.yml/badge.svg)](https://github.com/lockyc/imgkit/actions/workflows/ci.yml)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-555)
![Go](https://img.shields.io/badge/go-1.27%2B-00ADD8?logo=go&logoColor=white)
[![License](https://img.shields.io/github/license/lockyc/imgkit)](LICENSE)

Image and render operations for print and web pipelines, as one command:
lift a subject out of a photo, fill or enlarge artwork, render HTML to PNG and
PDF, make a press-ready PDF, match a colour grade. Each operation wraps the
best engine for the job at a pinned version, and its quality is measured
against a committed set of test images rather than judged by eye.

## Status

`version` and `doctor` work; the rest of the table is being built (see the
roadmap). The command set below is the design, not a feature list. The
build order is in [docs/roadmap.md](docs/roadmap.md), and how it is built is in
[docs/design.md](docs/design.md).

| Command | Does |
|---|---|
| `cutout` | Lifts the subject out of an image as RGBA, down to hair and fur |
| `inpaint` ⚠ | Fills a masked region with LaMa |
| `infill` ⚠ | Fills a hole in a flat ground by blending blurred copies of its surroundings, then adds grain to match |
| `upscale` ⚠ | Enlarges 4× with Real-ESRGAN |
| `grade fit` / `grade apply` | Recovers a reference image's colour grade as a lookup table, then applies it, keeping transparency |
| `render` | HTML to PNG or PDF with headless Chrome, guarded against Chrome's silent failures |
| `press` | Converts a PDF to a print master (text as outlines, CMYK colour) and checks it against a soft proof |
| `diff` | Compares two images after lining them up, so a small shift doesn't fail everything |
| `fonts` | Embeds web fonts into a CSS file so a page renders from the filesystem |
| `qr` | Makes a QR code SVG that any phone camera decodes |
| `doctor` | Checks every engine against its pin, installs the ones imgkit manages, and prints the install command for the rest |

⚠ These commands create pixels the source never had. A project that forbids
this puts `synthesis = "forbid"` in an `imgkit.toml`, and they refuse to run.

## Install

```bash
go install github.com/lockyc/imgkit@latest
```

## Development

```bash
just build    # ./imgkit
just test
just gate     # gofmt check + vet + tests
just quality  # quality cases over quality/ (needs every engine and model)
```
