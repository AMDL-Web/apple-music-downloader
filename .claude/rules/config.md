---
paths:
  - "configs/**"
  - "internal/config/**"
---

# Config

`configs/config.example.yaml` is the user documentation and the bootstrap source;
`configs/config.yaml` is generated and never documents anything. Every key carries
its full comment (allowed values, units, default when absent, runtime or startup),
renaming or removing a key is a breaking change, and a new key touches the struct, `Default()`, validation,
the example and possibly the job overrides together. The full rules are in
`internal/config/AGENTS.md`; read it before editing either directory.
