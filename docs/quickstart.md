# Local fixture quickstart

For field definitions, valid/invalid policies and content identity, see
[policy authoring](./policy-authoring.md).

This quickstart validates and executes one policy entirely on your machine. It
uses committed synthetic fixture evidence, performs no network access, reads no
credentials, and does not represent semantic model inference.

Build the development command through the repository build lock:

```sh
scripts/build
```

Validate the example YAML policy and print its canonical identity:

```sh
.tmp/bin/antaeus validate contracts/examples/v0alpha1/policy/vendor-onboarding.yaml
```

The output is one JSON object containing the policy name, RFC 8785/SHA-256
digest, and rule count.

Execute the exact `aggregate-analytics` fixture case:

```sh
.tmp/bin/antaeus evaluate \
  --policy contracts/examples/v0alpha1/policy/vendor-onboarding.yaml \
  --input contracts/examples/v0alpha1/input/aggregate-analytics.json \
  --fixture-set contracts/examples/v0alpha1/fixture-set/quickstart.json \
  --case aggregate-analytics
```

The command bounds and strictly parses the input JSON, canonicalizes it with
RFC 8785, verifies that the selected fixture case matches the exact policy and
input digests, evaluates every rule through the same provider-neutral boundary,
and prints one validated Decision JSON object. The Decision records
`mode: deterministic-fixture`, `synthetic: true`, and the fixture identity so it
cannot be confused with semantic evidence.

Run the same synthetic fixture through the portable regression contract:

```sh
.tmp/bin/antaeus test \
  --policy contracts/examples/v0alpha1/policy/vendor-onboarding.yaml \
  --suite contracts/examples/v0alpha1/regression-suite/quickstart.json \
  --fixture-set contracts/examples/v0alpha1/fixture-set/quickstart.json
```

This command prints a `RegressionResultSet`. A matching suite exits zero;
expectation mismatches still print the complete result set and exit two.
Configuration, loading, evaluator, and output errors exit one without a result
set.

An identity mismatch is an error. To add a local case, canonicalize the intended
input, record its SHA-256 digest and the policy digest in a FixtureSet, then
provide exact normalized rule results in policy order.
