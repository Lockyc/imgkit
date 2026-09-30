---
type: roadmap
links:
  - rel: see-also
    to: docs/design.md
    note: the architecture each phase builds toward
---

# imgkit — roadmap

Phases in build order. Each ships when its acceptance gate passes.

| Phase | Lane | Scope | Gate |
|---|---|---|---|
| 0 | Shipped | Repo, CLI stub, `version`, CI | `just gate` |
| 1 | Shipped | `internal/engine`, `internal/pins`, `doctor`, the `imgkit.toml` synthesis policy, the `quality/` harness | stub-engine tests; `doctor` reports this machine accurately |
| 2 | Not started | Deterministic ops: `render`, `press`, `diff`, `fonts`, `qr`, `grade apply` | unit tests and quality cases pass |
| 3 | Not started | ML ops: `cutout`, `grade fit`, `inpaint`, `upscale`, `infill` | quality cases pass at their recorded thresholds |
| 4 | Not started | `v0.1.0` release | release gate green; README curated |
| 5 | Not started | Regression check against the pipelines imgkit replaces, then each project's cutover | no measured regression; each cutover approved by its owner |

**Phase 5 changes nothing in a consuming project until its owner approves
that project's cutover.** The check runs imgkit beside the existing pipeline,
reading the project's assets without writing to its tree.

## Sequencing

Phases run in order. Within a phase, order is open.
