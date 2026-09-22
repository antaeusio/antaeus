# Metatron conventions (files-first)

This repo's coding conventions ("decisions") live as Open Knowledge Format markdown
under `context/decisions/`. In a monorepo each app has its own `context/` — use the
one **nearest** the files you are touching (walk up to the closest `context/`).

This repo's review gate is **`pr`** (`review_gate` in `metatron.toml`): decisions
are authored directly under `decisions/` on a working branch, and the repository's
ordinary pull-request review is the human curation act.

- **Consult first — required.** Before exploring or editing code in an area:
  1. run `cat context.md` (workspace root) for the decision index;
  2. open the relevant files with `cat context/decisions/<topic>.md`;
  3. only then plan — and state which decision files you read.
  A change that contradicts a decision will be rejected in review; listing the
  directory is not reading.
- **Record gaps as decisions — on a branch.** Found a durable convention that isn't
  captured? Author it as a new OKF file in the nearest `context/decisions/` on your
  working branch (skill: `context-okf-llm-ingest`). It reaches the default branch
  only through a human-reviewed pull request — never push decision changes to the
  default branch directly. Refining an existing decision? Edit that file on the
  same terms.
- **Optional staging.** `context/candidate/` remains available for proposals you are
  not ready to put in front of a reviewer; content there is never authoritative
  (skill: `context-okf-promote-candidates` covers moving staged files onward).
