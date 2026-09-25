# Compatibility and platform support

Antaeus is pre-release software. This document defines the compatibility boundary for development versions; release notes will identify what is actually available in each tagged version.

## Go module

The module path is `github.com/antaeusio/antaeus`. It requires Go 1.26.0 or newer and currently prefers Go 1.27.1 for development and release builds.

Before v1.0.0, documented public Go APIs and CLI behavior may change in a minor release with migration notes. Patch releases do not intentionally break compiling callers or documented behavior. From v1.0.0 onward, packages at the same import path will follow semantic versioning and Go's import compatibility rule.

Packages below `internal/`, unexported identifiers, test helpers, and undocumented automation commands are not public Go APIs.

## Portable contracts

Published policy, decision, evaluator-profile, JSON Schema, OpenAPI, and machine-output contract versions are immutable even before the Go module reaches v1. An incompatible data or semantic change receives a new contract version rather than silently changing an existing one.

## Release targets

Each release publishes:

| Artifact | Architectures | Minimum operating system |
| --- | --- | --- |
| macOS binary | amd64, arm64 | macOS 13 Ventura |
| Linux binary | amd64, arm64 | Linux kernel 3.2 |
| Windows binary | amd64 | Windows 10 or Windows Server 2016 |
| Linux container | amd64, arm64 | OCI-compatible Linux runtime |

Release binaries are pure Go and built with `CGO_ENABLED=0`, `GOAMD64=v1`, and `GOARM64=v8.0`. The same macOS and Linux archives back the Homebrew tap. The container image, `ghcr.io/antaeusio/antaeus`, packages the same Linux binaries on a digest-pinned distroless base that includes CA certificates, and runs as numeric user 65532 with `/work` as its working directory. It has no shell.

Other Go targets may compile, but they are unsupported until Antaeus publishes native validation and release artifacts for them. WSL uses the Linux binary and is not a separate target.

Release archives are cross-compiled on Linux and smoke-tested natively on Linux amd64, as is the amd64 container image. Other targets are compiled but not yet executed natively in the release workflow.
