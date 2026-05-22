# Qwen Code agent guardrails — background

This file pairs with CLAUDE.md / AGENTS.md and applies to Qwen Code
sessions operating in this submodule. The constitutional rules in
`CLAUDE.md` and `AGENTS.md` apply unchanged to Qwen agents.

For full context (Lava Sixth Law / Seventh Law inheritance, per-clause
enforcement) see this submodule's `CLAUDE.md` and the parent
`CONSTITUTION.md` / Helix Constitution at the parent project root.

## §107 — Anti-Bluff Forensic Anchor (parity with CLAUDE.md / AGENTS.md)

> **Verbatim operator mandate** (2026-04-29, reasserted multiple times across 2026-05; required to live in every submodule's Constitution.md, CLAUDE.md, AGENTS.md, **and QWEN.md**):
>
> "We had been in position that all tests do execute with success and all Challenges as well, but in reality the most of the features does not work and can't be used! This MUST NOT be the case and execution of tests and Challenges MUST guarantee the quality, the completion and full usability by end users of the product! This MUST BE part of Constitution of our project, its CLAUDE.MD and AGENTS.MD if it is not there already, and to be applied to all Submodules's Constitution, CLAUDE.MD and AGENTS.MD as well (if not there already)!"

**Operative rule applicable to Qwen Code sessions** working in this submodule:
the bar for shipping is not "tests pass" but **"users can actually use the feature."**
Every PASS in this codebase MUST carry positive runtime evidence captured during
execution. Metadata-only / configuration-only / absence-of-error / grep-based
PASS without runtime evidence are critical defects regardless of how green the
summary line looks. No false-success results are tolerable.

This anchor is the same text the existing CLAUDE.md and AGENTS.md carry —
QWEN.md was added later in the propagation cycle and gains parity here.
For the full ruleset (Lava Sixth + Seventh Laws inheritance, the per-clause
enforcement detail) see this submodule's `CLAUDE.md` and the parent
`CONSTITUTION.md`.
