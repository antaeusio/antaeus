# Local configuration precedence

`evaluator/localconfig` resolves already-loaded profiles and non-secret binding
artifacts for local runners. The [CLI configuration commands](./cli-configuration.md)
add manifest discovery, redacted inspection, explicit credential checks, and
saved project trust. The existing fixture CLI continues to use its documented
explicit inputs. Configuration checks do not execute evaluators.

## Selection

Select one complete profile, in order:

1. Explicit CLI selection.
2. Project configuration.
3. User configuration.
4. A command-specific built-in deterministic fixture, if the command explicitly
   permits one.
5. A configuration error with guidance to select a profile.

Profiles are never merged. The built-in fallback must contain only deterministic
fixtures; it cannot silently enable a semantic evaluator. Each supplied layer is
validated when constructing its snapshot, even when another level will shadow
it. Load authored artifacts through their strict loaders first, so duplicate
fields and conflicting definitions at the same level are rejected before they
can be lost in a language-native map. An invalid higher-priority source is an
error, not permission to use a lower-priority source.

Select one reference independently for each routed `(adapter ID, credential
slot)` pair, in order:

1. Explicit CLI reference.
2. Project reference.
3. User reference.
4. The installed adapter version's documented default environment reference.
5. A missing-credential configuration error.

The primary, escalation, and fallback routes all participate in credential
preflight. Unused bindings are ignored. Adapter defaults are keyed by exact
adapter ID and version, and are provided by the installed adapter contract.
If two routed versions share a slot but specify different defaults, resolution
fails; an explicit CLI, project, or user reference can resolve that conflict.
Selected variable names that differ only by letter case are rejected because
they are ambiguous on platforms with case-insensitive environment lookup.

A reference names an environment variable; it never contains a secret value.
An unset or empty winning variable is an error. It does not trigger a lookup
from a lower-priority source, retries, or an evaluator fallback. Values are
captured once per routed adapter/slot pair immediately before evaluation and
passed unchanged. Only invoked adapters receive their captured credential.

## Project trust

A project-selected profile or winning project reference requires trust before
any credentials can be read, including credentials selected from CLI references,
user references, or adapter defaults. Unused and fully shadowed project
configuration grants no authority and needs no trust for that invocation.
Credential-free fixture use requires no credential authority.

`NewLayer` takes an immutable snapshot of the full profile and binding artifacts.
The project snapshot must include all Antaeus configuration from that source,
including unused and shadowed bindings. Profile file contents are included,
not merely their paths, so changing the referenced profile revokes old trust.

The layer digest is lowercase `sha256:<64 hex digits>` over RFC 8785 canonical
JSON of this envelope:

```json
{
  "version": "antaeus.local-config/v0alpha1",
  "profile": null,
  "bindings": null
}
```

Replace each non-null member with its complete validated artifact. Both members
are always present. The absent layer (both artifacts absent) uses an empty digest.
This envelope defines the Go resolver's snapshot identity, not a configuration
file loader or a new profile schema. Source whitespace, map ordering, and file
paths do not change identity; changes to either artifact do. Reference names
are hashed but never printed by the ordinary summary.

The caller must obtain `TrustedProjectDigest` from explicit user approval or an
independent user trust store, never from the project itself. Approval must refer
to the exact snapshot the user inspected. Omitting that approval revokes trust
on the next resolution. A returned `Resolved` object retains that invocation's
approval; it must not be cached across invocations or used as a persistent trust
grant. Configuration edits require a new snapshot and new approval.

## Embedding sequence

1. Explicitly load profiles and reference artifacts using their strict loaders.
2. Build CLI, project, and user snapshots with `NewLayer`.
3. Call `Resolve` with the snapshots, independent project trust, and defaults
   registered for the installed adapter versions.
4. Inspect `Summary` for the winning profile source, profile digest, effective
   configuration digest, project digest when used, and per-slot source classes.
5. Obtain a profile copy with `Profile` and validate its parameters against the
   exact installed adapter versions before execution. This resolver does not
   install adapters, approve provider origins, or execute evaluator routing.
6. Call `Preflight` with an explicit environment lookup immediately before
   evaluation, then clear the returned credentials when evaluation ends.

The effective configuration digest uses the same snapshot envelope with the
selected profile and only winning routed references; absent bindings are null.
Changing a winning reference changes this digest without changing the profile
digest. Rotating the value behind an unchanged reference changes neither digest.
Source provenance is reported separately in `Summary`.

Never scan parent directories for `.env` files, automatically load `.env` files,
or enumerate unrelated process variables. Raw secret CLI flags are prohibited. Do not expose reference
names, values, paths, or unrestricted profile parameters in ordinary summaries.
Missing-credential errors may identify evaluator, adapter, slot, and source
class; the CLI must add remediation guidance without exposing reference names.

## Production boundary

Local precedence is not a production override mechanism. Production runners
must select an immutable deployment revision with exact policy/profile digests,
mode, operational limits, and declared secret references. They must not call
this local resolver or merge CLI, project, user, or ambient behavior settings
into a deployment. Environment or platform bindings may supply only the values
of its declared secrets. Changing evaluation behavior requires a new audited
deployment revision.

No production deployment resolver or hosted execution path is implemented by
this package. Enforcement of that boundary belongs to the production runner.
