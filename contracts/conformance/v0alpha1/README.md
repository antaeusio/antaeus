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

Additional YAML parser-restriction and resource-limit fixtures will land with the constrained YAML loader. CI remains network-free for all conformance cases.
