---
type: Metatron Decision
title: Constrained policy source loading
status: canonical
scope: policy/parser
confidence: high
source_refs:
  - policy/parse.go
  - contracts/README.md
  - contracts/conformance/v0alpha1/policy-source/
---

## Pattern

Load policy files through `policy.LoadFile` or parse bytes with `policy.Parse`.
Select JSON or YAML explicitly; file loading accepts only `.json`, `.yaml`, and
`.yml`. Enforce the published byte, depth, and aggregate-node limits before
typed decoding. Reject duplicate and non-string mapping keys, directives,
custom or non-JSON tags, anchors, aliases, merge keys, multiple documents,
invalid UTF-8, unknown properties, null optional strings, and non-finite or
unrepresentable numbers.

Resolve untagged YAML scalars with YAML 1.2.2 core-schema rules instead of the
parser library's legacy compatibility behavior. Convert the accepted tree to
the JSON data model, decode the closed v0alpha1 artifact, run semantic
validation, and only then canonicalize or compute its digest. Return stable,
bounded parser error codes and include source coordinates when available.

## Rationale

The authoring syntax must not change policy identity or permit parser-specific
graphs and native objects. A constrained tree-shaped loader makes YAML and JSON
produce the same validated artifact while bounding untrusted input and keeping
the language-neutral conformance fixtures authoritative.
