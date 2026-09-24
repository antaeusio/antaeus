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
   - records build-provenance attestations for every archive; and
   - publishes the GitHub release using that version's changelog section as
     notes. Tags with a pre-release suffix such as `-rc.1` are marked as
     pre-releases.
4. Update the Homebrew tap from the published checksums:

   ```sh
   gh release download vX.Y.Z --repo antaeusio/antaeus --pattern SHA256SUMS --dir /tmp/antaeus-vX.Y.Z
   scripts/homebrew-formula vX.Y.Z /tmp/antaeus-vX.Y.Z/SHA256SUMS > <tap checkout>/Formula/antaeus.rb
   ```

   Commit the formula to `antaeusio/homebrew-tap`, then run
   `brew install antaeusio/tap/antaeus && brew test antaeus`.
5. Verify installation from a clean environment with each supported method
   before announcing the release.

To rehearse locally without publishing, run
`scripts/release-artifacts vX.Y.Z-rc.0` from a clean checkout; output goes to
`.tmp/release/`.
