# Releasing

Releases are cut by pushing a semantic-version tag to a commit on `main`.
Maintainers push release tags; the workflow refuses tags that are not on
`main` history.

1. Move the `## [Unreleased]` entries in `CHANGELOG.md` under a new
   `## [X.Y.Z] - YYYY-MM-DD` heading that begins with a short summary, and
   update the compare links at the end of the file. Merge that change.
2. Tag the merged commit and push the tag:

   ```sh
   git tag -a vX.Y.Z -m "Antaeus vX.Y.Z"
   git push origin vX.Y.Z
   ```

3. The [release workflow](../.github/workflows/release.yml) then:
   - refuses tags that are not on `main` history;
   - runs `scripts/check`;
   - builds archives with `scripts/release-artifacts`, which injects the
     version and commit and pins the documented target baselines;
   - smoke-tests the Linux amd64 archive and verifies `SHA256SUMS`;
   - records build-provenance attestations for every archive;
   - builds the linux/amd64 and linux/arm64 container image from those same
     archived binaries, pushes it to `ghcr.io/antaeusio/antaeus` by digest,
     smoke-tests and attests that digest, then tags it `vX.Y.Z`;
   - publishes the GitHub release using that version's changelog section as
     notes. Tags with a pre-release suffix such as `-rc.1` are marked as
     pre-releases; and
   - after the release exists, moves `X.Y` and `latest` to the image when this
     is the newest release in its minor line or overall.
4. Update the Homebrew tap from the published checksums:

   ```sh
   gh release download vX.Y.Z --repo antaeusio/antaeus --pattern SHA256SUMS --dir /tmp/antaeus-vX.Y.Z
   scripts/homebrew-formula vX.Y.Z /tmp/antaeus-vX.Y.Z/SHA256SUMS > <tap checkout>/Formula/antaeus.rb
   ```

   Commit the formula to `antaeusio/homebrew-tap`, then run
   `brew install antaeusio/tap/antaeus && brew test antaeus`.
5. Verify installation from a clean environment with each supported method
   before announcing the release.

The first time the image is published, GitHub creates the
`ghcr.io/antaeusio/antaeus` package as private. An organization owner must make
it public once (package settings → Change visibility), and step 5 must include
an anonymous `docker pull`. Rehearse container changes with a pre-release tag
such as `vX.Y.Z-rc.1`, which pushes only its own image tag. `latest` and `X.Y`
move only after the smoke test, attestation, and release succeed, and only to
the newest release overall or within that minor line.

If a run fails after the release is created, re-run the publish job: an
existing release is left unchanged. The re-run pushes and attests a fresh
digest (image timestamps come from the commit time) and moves the tags to it.
To move floating tags by hand, run
`docker buildx imagetools create -t ghcr.io/antaeusio/antaeus:X.Y -t ghcr.io/antaeusio/antaeus:latest ghcr.io/antaeusio/antaeus@<digest>`
with the digest from the run log.

To test the container locally, run `scripts/container-image vX.Y.Z-rc.0`
from a clean checkout with Docker running; it loads `antaeus:vX.Y.Z-rc.0` for
the host architecture.

To rehearse locally without publishing, run
`scripts/release-artifacts vX.Y.Z-rc.0` from a clean checkout; output goes to
`.tmp/release/`.
