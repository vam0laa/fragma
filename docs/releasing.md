# Releasing

## Making a release

1. Set `const version` in `main.go`, merge to `main`, and see that the tests pass there.
2. Tag that commit and push the tag: `git tag v0.5.0 && git push origin v0.5.0`.
3. The `release` workflow tests, builds, checks the files, records their provenance and creates a
   **draft** release. It stops if the tag does not match `version` or is not on `main`.
4. Open the draft, check the notes and the five files, and publish it. Until then the installer
   keeps getting the previous release.

A push that changes the workflow, `scripts/release/`, the `Makefile` or `go.mod` runs everything
except provenance and the draft. `make dist` builds and checks the same files on your computer; with
the Go named by `toolchain` in `go.mod` the checksums match those from GitHub.

## Verifying a download

`SHA256SUMS` shows that a file arrived whole. It comes from the same release, so it does not show
who made the file. Provenance does: it states that this repository's release workflow built it.
With the [GitHub CLI](https://cli.github.com):

```bash
gh attestation verify fragma-linux-amd64.tar.gz --repo vam0laa/fragma \
  --signer-workflow vam0laa/fragma/.github/workflows/release.yml
```

Releases up to 0.4.0 were uploaded by hand and have no provenance.

## Protecting releases (repository settings)

These are settings of the repository, not files in it. **They are not applied by this document**;
the owner has to set them.

**Only administrators create, move or delete version tags** (Settings → Rules → Rulesets, or):

```bash
gh api -X POST repos/vam0laa/fragma/rulesets --input - <<'JSON'
{
  "name": "version tags",
  "target": "tag",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["refs/tags/v*"], "exclude": [] } },
  "rules": [ { "type": "creation" }, { "type": "update" }, { "type": "deletion" } ],
  "bypass_actors": [ { "actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always" } ]
}
JSON
```

(`actor_id` 5 is the administrator role.)

**Published releases cannot be changed**: Settings → General → Releases → *Enable release
immutability*.

**Protect `main`** (require a pull request and the tests), so that what gets tagged has been
reviewed.

What these do not cover: a collaborator with write access can still edit a draft, or create a
release for a new tag whose name does not start with `v`, and the installer takes whatever release
is marked *latest*. Give write access only to people you trust with that, and verify provenance
when it matters.
