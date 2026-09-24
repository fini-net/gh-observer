# Project Governance

gh-observer is a maintainer-led project. Final decisions on direction,
scope, and merges rest with the project owner (@chicks-net). Day-to-day,
the project runs on rough consensus: anyone may propose a change via a
pull request, and proposals are discussed openly in the PR and in issues
before the maintainer accepts or declines them.

## Roles

- **Maintainer / project owner** (@chicks-net): sets roadmap and scope,
  reviews and merges pull requests, triages issues and security reports,
  and cuts releases. Holds admin on the fini-net organization and this
  repository.
- **Contributors**: anyone submitting issues, discussions, or pull
  requests. Contributions follow the requirements in
  [CONTRIBUTING.md](../.github/CONTRIBUTING.md) (DCO sign-off, tests for
  major changes, passing CI).

## Succession

The repository lives in the fini-net GitHub organization. Organization
owners can administer any repository in it: create and close issues,
accept proposed changes, and publish releases. The release process is
fully scripted (see the release recipe in the
[justfile](../justfile)), and signing is keyless (Sigstore), so no
private signing keys need to be handed over.

The intent is to add a second organization owner and to grow co-maintainers
from the contributor base so that continuity does not depend on a single
person.

## Decision disputes

Disagreements are resolved by discussion in the relevant issue or PR;
if discussion does not produce agreement, the maintainer decides.
