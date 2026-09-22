# v0alpha1 conformance fixtures

Every implementation claiming v0alpha1 support must consume the same files in this directory and the valid examples under `../../examples/v0alpha1/`.

## Canonical policy

Both of these authoring forms represent the same policy:

- `../../examples/v0alpha1/policy/vendor-onboarding.yaml`
- `../../examples/v0alpha1/policy/vendor-onboarding.json`

They must produce the exact UTF-8 bytes in `canonical/vendor-onboarding.canonical.json` after validation and RFC 8785 canonicalization, and the digest in `canonical/vendor-onboarding.sha256`.

## Invalid policies

- `policy/invalid-empty-rules.json` fails structural schema validation.
- `policy/invalid-unknown-property.json` fails structural schema validation.
- `policy/invalid-duplicate-rule-id.json` passes structural validation but fails the semantic uniqueness invariant.

## Invalid decisions

- `decision/invalid-failure-missing-details.json` fails because a failure outcome requires a structured failure object.
- `decision/invalid-non-match-outcome.json` fails because only a matched rule carries its policy outcome.

## Deterministic reduction

`reduction/cases.json` contains one policy plus ordered rule-result inputs and expected reduced outputs. Implementations must reproduce the specified outcome, required reason codes, and unresolved-rule failure exactly. The cases cover every precedence step, including matched deny over unresolved evidence and unresolved evidence over matched review.

## Invalid fixture sets

- `fixture-set/invalid-unknown-property.json` fails closed on an unknown rule-result property.
- `fixture-set/invalid-bad-digest.json` rejects a non-digest policy identity.
- `fixture-set/invalid-empty-rule-results.json` rejects a case without exact rule evidence.

## Policy source parsing

`policy-source/valid-core-scalars.yaml` confirms YAML 1.2 core-schema behavior
for plain strings that YAML 1.1 parsers commonly misresolve. The invalid YAML
fixtures cover duplicate and non-string keys, directives, tags, anchors,
aliases, merge keys, multiple documents, and non-finite numbers. Generated
tests cover invalid UTF-8 and the byte, depth, and aggregate-node limits. CI
remains network-free for all conformance cases.
