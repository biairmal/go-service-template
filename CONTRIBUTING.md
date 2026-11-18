# Contributing

How a change gets from an idea to `main`. It applies to everyone, human or AI agent. How the code itself is written
(layout, layering, module rules, test conventions) is in [AGENTS.md](AGENTS.md), and it binds humans too.

This page is about changing the template itself. A client project started from it with `gonew` keeps this page and
adapts it.

## Before you start

1. Get the stack running with the README's [Run locally](README.md#run-locally).
2. Read [AGENTS.md](AGENTS.md) for the code rules, [docs/PATTERNS.md](docs/PATTERNS.md) for examples, and
   [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for how it fits together.
3. Find the spec. The template's spec is the workspace's Phase 1 `SPEC.md` and implementation plan; identity's TRD
   covers the parts this template shares with it. When a spec and AGENTS.md disagree, the spec wins. When the spec
   is unclear, pick the simplest option that fits it and add a line to [docs/DECISIONS.md](docs/DECISIONS.md).

## Git conventions

`.github/workflows/pr-conventions.yml` checks these on every PR. PRs are squash-merged, so the PR title becomes the
commit on `main`.

- **Branch:** `<type>/<group>/<task-id>-<slug>`. `<group>` is optional and names the work stream, so parallel streams
  don't collide. The task ID keeps the plan's casing. Examples: `feat/p1/T4.4-list-filters`, `fix/deps/klauspost-cve`.
- **Commit and PR title:** [Conventional Commits](https://www.conventionalcommits.org/),
  `<type>(<scope>)?: <what it does, lower case>`, plus ` (<task id>)` on the PR title. Types:
  `feat fix refactor perf test docs build ci chore revert`. A breaking change adds `!` and a `BREAKING CHANGE:`
  footer. The commit body says why, not how.
- **PR body:** fill in [.github/pull_request_template.md](.github/pull_request_template.md). `### Why` (requirement,
  bug, security, or refactor, with its source), `### What changed` and `### Verification` are required. Verification
  quotes each command's real output from the final pushed head, keeps failed or skipped checks in the table, and
  lists what was not verified.
- **Releases:** tag `vX.Y.Z` on `main`. CI publishes the image with that tag, and `/version` returns it.

## Definition of Done

- [ ] `make check` passes; `make test-integration` passes when the change touches the database.
- [ ] New behaviour has table-driven tests.
- [ ] New exported symbols have doc comments.
- [ ] No module-to-module imports; no secrets in logs.
- [ ] The README, `.env.example`, `api/swagger`, [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and
      [SECURITY.md](SECURITY.md) match the change when it touches setup, config, commands, the API or the structure.
