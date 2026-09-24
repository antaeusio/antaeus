# CLI configuration and project trust

Run from the intended project root:

```sh
antaeus config inspect
antaeus config trust --digest sha256:<digest-from-inspection>
antaeus config check
antaeus config revoke --digest sha256:<previously-approved-digest>
```

Replace each digest placeholder with the full inspected value. Review the
manifest, profile, and binding artifacts before trusting. `inspect` prints
redacted source selections, configuration identity, project digest, actual saved
trust status, and whether credential access needs approval. It never reads
credential values or prints environment-reference names.

`trust` requires the supplied digest to match the current complete snapshot.
Edits between inspection and approval are rejected. `revoke` accepts the old
digest even after files change or become invalid. Commands emit JSON on success,
exit 64 for invalid arguments, and exit 1 with stderr diagnostics for
configuration errors.

`check` validates saved trust before checking every routed reference in the
existing process environment. Each value must be set and non-empty. Captured
values are cleared before returning and never printed. This checks credential
presence only: installed-adapter parameter validation, provider authentication,
network requests, and semantic evaluation remain separate execution requirements.
The installed OpenAI adapter provides a default reference for its
`openai-api-key` slot (the `OPENAI_API_KEY` variable, see
[the adapter guide](./openai-adapter.md)); other credentialed profiles need
explicit reference bindings, which also override the default. Missing credentials include remediation guidance
without exposing names or values. An unset winning reference does not fall back
to a lower-priority source.

Inspection and checks accept `--profile <file>` and `--bindings <file>` as
explicit selections. Repeated, empty, and unknown flags are rejected. Raw secret
flags do not exist. An explicit profile using project-provided bindings still
needs project trust; overriding both selections does not grant lasting trust.
The existing fixture `evaluate` and `test` commands retain their explicit,
credential-free behavior and do not consume these manifests.
The separate [`evaluate-profile` command](./profile-execution.md#cli-fixture-profiles)
uses this selection workflow and the profile runner. It installs the
deterministic fixture adapter, which never reads credentials, and the OpenAI
adapter. Profiles using any other adapter are rejected without credential-setup
or trust-grant prompts, even if project trust was previously granted. An OpenAI
profile whose credential authority comes from project configuration runs only
after saved trust for that exact project snapshot; execution reads saved trust
but never grants it. Use `config` commands separately when deliberately
inspecting or managing configuration; execution never grants project trust.

## Locations and manifest

The project manifest is `.antaeus/config.json` under the current directory.
There is no parent or Git-root search. The user manifest is `antaeus/config.json`
under Go's `os.UserConfigDir()`:

- macOS: `$HOME/Library/Application Support/antaeus/config.json`;
- Linux: `$XDG_CONFIG_HOME/antaeus/config.json`, or
  `$HOME/.config/antaeus/config.json` when XDG_CONFIG_HOME is unset;
- Windows: `%AppData%\antaeus\config.json`.

These OS variables locate configuration; they cannot override profile behavior.
The user directory and trust storage must remain outside the project, including
after resolving symlinks.

The closed [LocalConfiguration schema](../contracts/schemas/v0alpha1/local-configuration.schema.json)
defines the manifest:

```json
{
  "apiVersion": "config.antaeus.io/v0alpha1",
  "kind": "LocalConfiguration",
  "profileFile": "../profile.json",
  "bindingsFile": "../bindings.json"
}
```

At least one file field is required. Relative paths resolve from the manifest
directory. Artifacts must use lowercase `.json`, `.yaml`, or `.yml` extensions.
Manifests are strict JSON limited to 16 KiB, with paths of at most 4,096 Unicode
code points and no NUL characters. All files must be regular files. Unknown,
duplicate, case-variant, null, and wrongly typed fields are errors. Manifests
contain no trust grants, secrets, environment-variable values, endpoint overrides,
or embedded evaluator settings.

Project manifests and referenced artifacts are read through a directory-rooted
filesystem handle. Paths and symlinks cannot escape the current project. User
manifests and explicit CLI paths may reference artifacts elsewhere. An absent
manifest is an empty level; malformed or unreadable configuration is an error,
even when another level would override it. No command searches for or loads
`.env` files or enumerates unrelated credential variables.

Profile and binding contents participate in snapshot identity; path spelling
alone does not. See [configuration precedence](./local-configuration.md) for the
exact digest envelope, ordering, and production boundary.

## Trust storage

Approvals are scoped to the symlink-resolved absolute project-directory path and exact snapshot
digest under the user directory's `trust/` folder. Moving or copying a project
requires new approval. Each grant uses a separate marker to avoid overwriting
unrelated grants. New directories and files request owner-only permissions,
subject to platform filesystem semantics; manage existing directory permissions
as part of the user's configuration.

Markers contain only a version marker and hashed identity, not project paths,
reference names, or secret values. Corrupt or symlinked markers fail closed;
revoke and reapprove the digest to repair one. Trust storage is user-controlled
and must never be populated from project files. It is not a defense against
another process that already has write access to the user's configuration.

Grant creation prepares, syncs and closes a private temporary file before
atomically linking it to the approval name without replacing an existing entry.
The trust filesystem must support same-directory hard links; otherwise granting
trust fails closed. Interrupted preparation may leave an ignored
`.pending-approval-*` file, never a partial approval. This is atomic visibility,
not a guarantee of persistence across power loss. Concurrent revocation can
invalidate an observed grant; trust storage is not an interprocess execution lease.

Containment of user configuration and trust storage is checked using existing
ancestor directory identities, including case aliases, not just path spelling.
Symlinked project paths share approval with their resolved target. Other aliases
(such as alternate casing or Windows short names) can still require a separate
approval because the trust key retains the resolved absolute spelling. Native
Windows junction/short-name behavior is not established by cross-compilation.

All commands refuse a user-config directory nested inside the current project,
including when invoked from a parent such as the home directory. There is no
grant-listing or moved/deleted-project revocation command. A corrupt marker also
blocks inspection with explicit overrides; inspection never repairs trust.

## Development migration for the planned v0.1.0 release

Existing complete approval markers and their keys remain valid. Published schemas,
CLI flags and successful output shapes are unchanged. Granting trust now requires
hard-link support; use an independent user-config location on a supporting
filesystem if the command reports that requirement. Corrupt legacy markers still
require explicit revocation and reapproval.

A dangling intermediate directory symlink (for example `.antaeus` pointing at a
missing directory) is now a configuration error rather than an absent project
manifest that could select user configuration. Repair the link or remove the
invalid configuration deliberately; the CLI does not silently choose a lower
layer. User/trust storage reached through a case alias into the project is also
rejected. No release is claimed by this migration note.
