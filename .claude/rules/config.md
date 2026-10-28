---
paths:
  - "configs/**"
  - "internal/config/**"
---

# Config

`configs/config.example.yaml` is the user documentation and the bootstrap source;
`configs/config.yaml` is generated and never documents anything. Every key carries
its full comment (allowed values, units, runtime or startup), `Default()` equals the
example's value for every key except the album and artist path formats, renaming or
removing a key is a breaking change with no compatibility shim, and a new key touches
the struct, `Default()`, validation, the example and possibly the job overrides
together. The full rules are in `internal/config/AGENTS.md`; read it before editing
either directory.
