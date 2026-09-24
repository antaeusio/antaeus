# Authoring a policy artifact

A policy describes semantic conditions and their business outcomes. It does not
select a model, hold credentials, authorize an operation or execute application
side effects. Deterministic checks belong in ordinary application code when they
can express the requirement completely.

This guide describes the implemented `policy.antaeus.io/v0alpha1` artifact.
The [policy schema](../contracts/schemas/v0alpha1/policy.schema.json),
[portable contract rules](../contracts/README.md) and conformance fixtures remain
the source of truth. Validation proves structural and semantic invariants, not
that a policy is clear, correct for a business or accurately evaluated by a model.

## A valid policy

The committed [vendor-onboarding YAML](../contracts/examples/v0alpha1/policy/vendor-onboarding.yaml)
contains:

```yaml
apiVersion: policy.antaeus.io/v0alpha1
kind: Policy
metadata:
  name: vendor-onboarding
  description: Route vendor submissions according to review policy.
spec:
  defaultOutcome: review
  rules:
    - id: prohibited-service
      description: Deny services in a prohibited category.
      when: The submitted service belongs to a prohibited category.
      outcome: deny
    - id: complete-low-risk-submission
      description: Allow a complete submission with no material risk indicators.
      when: The submission is complete and contains no material risk indicators.
      outcome: allow
```

The [JSON equivalent](../contracts/examples/v0alpha1/policy/vendor-onboarding.json)
has the same canonical identity. This is a plumbing example: terms such as
“prohibited category” and “material risk indicators” need concrete definitions
and measured evaluation before a real application could rely on them.

| Field | Authoring rule |
| --- | --- |
| `apiVersion`, `kind` | Exactly `policy.antaeus.io/v0alpha1` and `Policy` |
| `metadata.name` | Matches `[a-z][a-z0-9-]{0,62}`; a name is not a content version |
| `spec.defaultOutcome` | Explicit `allow`, `review` or `deny` |
| `spec.rules` | Ordered list of 1–256 rules |
| Rule `id` | Matches `[a-z][a-z0-9._-]{0,63}` and is unique within the policy |
| Rule `when` | Nonblank condition, at most 16,384 Unicode code points |
| Rule `outcome` | `allow`, `review` or `deny`, applied only to matched evidence |
| Optional descriptions | Strings of at most 4,096 Unicode code points; omit rather than use `null` |

Unknown properties and case variants are rejected. Rule IDs are explicit stable
identifiers, not array positions or generated names; changing one changes the
artifact and any corresponding fixture or regression identity.

Rules are not a first-match program. After evaluator routing completes, the
reducer checks matched deny first, then unresolved evidence, matched review,
matched allow, and finally the default when all rules are not matched.
Unresolved evidence produces `failure` unless a matched deny takes precedence;
it does not use `defaultOutcome`. `failure` is an execution result, not an allowed
policy-authored outcome. See the [exact reduction rules](../contracts/README.md#deterministic-reduction).

## Validate without credentials

From the repository root, using the [development prerequisites](../CONTRIBUTING.md#local-contribution-workflow):

```sh
scripts/build
.tmp/bin/antaeus validate contracts/examples/v0alpha1/policy/vendor-onboarding.yaml
.tmp/bin/antaeus validate contracts/examples/v0alpha1/policy/vendor-onboarding.json
```

Both print the same JSON identity summary and exit zero:

```json
{"name":"vendor-onboarding","digest":"sha256:2378b5a1806bb11c618bd3a78122e773ec93bdfc45b751a18887d40f4ea536a2","rules":2}
```

Validation makes no evaluator call. Continue to the [local quickstart](./quickstart.md)
to run synthetic fixtures; a fixture result tests execution plumbing, not semantic
quality. Loading, validation and output errors exit one with diagnostics on stderr;
usage errors exit 64. No valid identity summary is emitted for an invalid policy.

## Invalid examples and where they fail

These files are intentionally invalid. Run each command separately; each should
exit one, write a diagnostic to stderr and produce no identity JSON on stdout.

```sh
.tmp/bin/antaeus validate contracts/conformance/v0alpha1/policy-source/invalid-duplicate-key.yaml
.tmp/bin/antaeus validate contracts/conformance/v0alpha1/policy/invalid-unknown-property.json
.tmp/bin/antaeus validate contracts/conformance/v0alpha1/policy/invalid-duplicate-rule-id.json
```

| Example | Why it is rejected | Go error boundary |
| --- | --- | --- |
| [Duplicate YAML key](../contracts/conformance/v0alpha1/policy-source/invalid-duplicate-key.yaml) | Contains `kind: Policy` twice, even though the values agree | `*policy.ParseError`, `source.duplicate_key` |
| [Unknown property](../contracts/conformance/v0alpha1/policy/invalid-unknown-property.json) | Adds `metadata.owner`, which is not a v0alpha1 field | `*policy.ParseError`, `source.schema` |
| [Duplicate rule ID](../contracts/conformance/v0alpha1/policy/invalid-duplicate-rule-id.json) | Uses `same-rule` for two different conditions | `*policy.ValidationError`, `rule_id.duplicate`, path `$.spec.rules[1].id` |

JSON Schema alone does not enforce every rule: duplicate rule IDs need semantic
validation, and source-level duplicate keys must be rejected before a decoder
could discard them. These Go error codes are not a portable cross-language error
taxonomy. Parse errors include one-based source coordinates when available;
coordinates are zero when unavailable. Do not parse human error messages as an API.

## Constrained source syntax

Use UTF-8 JSON or one constrained YAML 1.2.2 document representing the JSON data
model. File loading accepts `.json`, `.yaml` and `.yml`. Duplicate or non-string
keys, anchors, aliases, merge keys, directives, custom/non-JSON tags, multiple
documents, invalid Unicode and unknown fields are rejected. JSON also rejects
unpaired surrogate escapes and trailing documents. YAML source line breaks must
be LF or CRLF; bare CR and literal U+0085, U+2028 and U+2029 are rejected anywhere
in YAML source. This is not support for every feature of a general YAML parser.

Source is bounded to 1 MiB, container depth to 32 and parsed nodes to 10,000.
Depth includes the root container; node counting includes mapping keys as well
as containers and values. Limits are inclusive. The current YAML implementation
composes a tree within the source-byte cap before its node/depth walk; those
checks are not a streaming allocation guarantee.

Policy fields contain no numbers. Shared parsing still rejects non-finite,
unrepresentable, underflow-to-zero and out-of-safe-range numeric values before
typed validation. Quote textual scalars when YAML would otherwise resolve them
as a different JSON type. See the full [resource and source rules](../contracts/README.md#resource-limits).

## Identity and revision

`Artifact.CanonicalJSON` validates the typed artifact and emits RFC 8785 canonical
JSON. `Artifact.Digest` hashes those UTF-8 bytes as lowercase `sha256:<64 hex digits>`.
Equivalent parsed YAML/JSON, source comments and mapping-key order do not change
identity. Whitespace or quoting changes preserve identity only if the parsed
string values stay identical. Array order, string code points, descriptions,
rule IDs, conditions and outcomes are part of the content; editing them changes
the digest. No Unicode normalization or removal of descriptive fields occurs.

A registry version label, when supplied to an execution API, is separate from
the policy artifact and is not a substitute for its digest. Update digest-bound
fixtures and regression suites deliberately after an edit; do not relax their
identity checks to reuse stale evidence. A new policy revision does not require
a new schema version, but an incompatible portable contract change does.

## Go callers

Use `policy.LoadFile(path)` for a file or `policy.Parse(bytes, policy.FormatJSON)`
or `policy.FormatYAML` for bytes. These enforce source constraints before typed
decoding and semantic validation. Directly unmarshalling into `policy.Artifact`
and calling `Validate` cannot recover discarded duplicate/unknown source fields.
The typed `Validate` method is not a replacement for the bounded source loader.

The public-package [executable examples](../policy/example_test.go) demonstrate
identical YAML/JSON digests and typed rejection of the three files above:

```sh
scripts/with-build-lock go test -count=1 -run '^ExampleLoadFile' ./policy
```

The [evaluator guide](./evaluator-adapters.md) explains how validated policy rules
become evidence requests without disclosing their authored outcomes to evaluators.
