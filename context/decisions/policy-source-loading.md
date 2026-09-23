---
type: Metatron Decision
title: Constrained policy source loading
status: canonical
scope: policy/parser
confidence: high
source_refs:
  - policy/parse.go
  - internal/strictsource/source.go
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
unrepresentable numbers. Restrict YAML source line breaks to LF or CRLF and
reject bare CR, U+0085, U+2028, and U+2029 everywhere so legacy parser folding
cannot alter accepted scalar text.

Depth counts active object/array containers, including the root container;
aggregate nodes count containers, scalar values, and mapping keys. The current
YAML dependency composes a complete tree before the node/depth walk. The 1 MiB
source cap bounds that allocation, and this is an accepted pre-release tradeoff;
do not raise the cap or expose untrusted remote YAML parsing without replacing
it with a streaming preflight or parser.

Resolve untagged YAML scalars with YAML 1.2.2 core-schema rules instead of the
parser library's legacy compatibility behavior. Convert the accepted tree to
the JSON data model, decode the closed v0alpha1 artifact, run semantic
validation, and only then canonicalize or compute its digest. Return stable,
bounded parser error codes and include source coordinates when available.

The shared strict-source decoder normalizes JSON and YAML numeric spellings to
their RFC 8785 binary64 representation. It rejects non-finite or
unrepresentable values and any value whose normalized magnitude exceeds the
interoperable IEEE-754 safe-integer range. Current policy fields contain no
numbers; the rule keeps every contract using the shared decoder portable.

## Rationale

The authoring syntax must not change policy identity or permit parser-specific
graphs and native objects. A constrained tree-shaped loader makes YAML and JSON
produce the same validated artifact while bounding untrusted input and keeping
the language-neutral conformance fixtures authoritative.
