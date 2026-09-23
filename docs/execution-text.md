# Execution metadata text

Published schemas remain unchanged. Text validation is field-specific; there is
no universal Antaeus whitespace rule and no new model-ID or control-character
ban. Strings are not trimmed or normalized before recording them.

| Boundary | Nonblank rule |
| --- | --- |
| Execution-trace provider, requested/resolved model and revision | Existing `.*\S.*` pattern, interpreted as ECMA-262 |
| Decision adapter version and fixture version | Existing `.*\S.*` pattern, interpreted as ECMA-262 |
| Go result metadata provider/model/revision and adapter/fixture versions | Same ECMA-262 rule, with valid UTF-8 and existing 128/256-code-point bounds |
| Go Decision provider/model validation | Same execution-text rule to preserve accepted result metadata through Decision assembly; their schemas retain their existing length-only constraints |
| Authored profile descriptions/component versions/fixture parameters and regression descriptions/versions | Their existing explicit Unicode-whitespace patterns; unchanged |
| Other fields, including request conditions, correlation IDs, request IDs, registry labels and common version definitions | Existing field-specific validation; unchanged |

JSON Schema's [pattern keyword](https://json-schema.org/draft/2020-12/json-schema-validation#section-6.3.3)
uses the ECMA-262 regular-expression dialect. Its `\S` complement excludes
ECMAScript whitespace and line terminators, including U+FEFF, but not U+0085.
See [ECMAScript whitespace](https://tc39.es/ecma262/2026/multipage/ecmascript-language-lexical-grammar.html#sec-white-space)
and [character class escapes](https://tc39.es/ecma262/multipage/text-processing.html#sec-characterclassescape).
Go's `strings.TrimSpace` and default regexp `\S` are different predicates.
For example, U+FEFF-only execution text is blank, U+0085-only execution text is
nonblank, and U+0000 remains accepted. These rules are not a display-safety or
sanitization guarantee. Renderers must handle metadata safely in their own context.
Normal text surrounded by whitespace keeps its exact bytes.

Requested profile model/revision identifiers retain their narrower existing
grammar. Returned resolved metadata is not constrained to that identifier
grammar. Empty optional Go metadata still means absent; present JSON strings
must satisfy their schema's minimum length. All length limits count Unicode
code points, not bytes or UTF-16 code units.

## Development migration for planned v0.1.0

This corrects implementation disagreement with the existing portable patterns;
it does not edit a schema, change a trace version or redefine profile identity.
The Go behavior change belongs to the planned first minor v0.1.0, not a patch:
execution metadata composed only of ECMAScript whitespace (notably U+FEFF) is
now rejected, while U+0085 can make that metadata nonblank. Results accepted by
the older Go validator may therefore become `evaluation.invalid_result` in the
profile runner. A formerly rejected resolved model/revision can instead be
accepted and contribute real rule evidence. Existing operational failure and
deny-precedence rules still apply; this does not force every invalid result to
override an earlier accepted deny.

Adapter/fixture versions invalid at the result boundary are rejected by runner
preflight before acceptance, even when an authored profile's distinct version
rule permits them. `evaluator.ValidateResult`, `decision.Decision.Validate` and
the single-attempt `evaluator.Decide` path share the affected execution metadata
checks; this is **not** limited to `runner.Run`. The fixture-only CLI can surface
those validation changes for unusual fixture versions, although it has no remote
model adapter. Consumers should supply meaningful versions/model labels and not
rely on a particular language's treatment of blank text. No release is claimed.

## Conformance checks

Go tests consume `contracts/conformance/v0alpha1/text/whitespace.json` for result,
Decision, runner and schema validation. The test-only schema adapter corrects
only the repository's exact `.*\S.*` pattern, using an explicit ECMA whitespace
class; it rejects other `\s`/`\S` patterns pending review. It is not a general
ECMAScript regexp implementation and does not alter schema files. Production
text checks use a separate rune predicate, not that test adapter.

Run `scripts/check` for the Go suite. Additionally, with Node.js installed, run
`node scripts/check-text-conformance.mjs`. This offline, dependency-free companion
loads the same cases and actual published string subschemas and uses native
JavaScript RegExp, reference/allOf resolution and code-point length checks. It
checks those selected string subschemas, not complete JSON documents; Go tests
also validate complete emitted execution traces. Node is not required to build
the Go library or CLI, and this companion is an explicit additional check, not
silently skipped by the Go suite.
