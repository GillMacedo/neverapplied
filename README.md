# neverapplied: find SQL migrations your database will skip, refuse or run out of order

[![CI](https://github.com/GillMacedo/neverapplied/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/neverapplied/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**A Claude Code skill and Go CLI for teams using golang-migrate, goose or dbmate. It compares a branch's migrations folder with the base branch and reports migrations that databases already deployed from main won't run as written: a new file numbered below main's latest version, an edit to a migration that already ran, a removed or renamed version, two files with one version, and files the tool ignores because of their names.**

Migration tools record which versions a database has applied, not what the files said. Two branches each add a migration, the later-numbered one merges first and is deployed, then the other merges. Its version is now below the database's. golang-migrate stores only the latest version, so it never runs that file, and its `Up` returns `no change`. goose stops the deploy with `found 1 missing migrations`. dbmate runs it after the newer one, in a different order from a fresh database. An edit to a migration that already ran is never applied by any of the three, so staging and production keep the old schema while new databases get the new one. Code review misses this because each file looks fine on its own.

`neverapplied` reads the two folders, or one folder and a git revision, and says what each tool will do. It doesn't connect to a database and doesn't need one.

## What you can ask Claude

- "Review the migrations in this PR against main."
- "My migration ran locally but staging never got the column. Why?"
- "I rebased onto main and now goose says found 1 missing migrations. What happened?"
- "Is it safe to edit this migration, or do I need a new one?"
- "Which version should I give this migration after the merge?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/neverapplied
    claude plugin install neverapplied@neverapplied-marketplace

The skill builds the tool from source with Go 1.22 or newer. It has no dependencies. To run it yourself, clone the repo and use `go run ./plugin/scripts`, or `sh plugin/scripts/run.sh` from any folder. In CI, after fetching the base branch:

    git fetch origin main
    sh plugin/scripts/run.sh --base-ref origin/main db/migrations

## Example

`examples/golang-migrate` is a made-up shop schema. `base` is main after a migration `20251103120000_add_orders_status` was merged and deployed. `head` is a branch merged with main: it adds an index migration created on 20 October, edits the orders table migration that main already ran, and adds a backfill whose name has no `.up`:

    $ go run ./plugin/scripts --fail-on never --base examples/golang-migrate/base examples/golang-migrate/head
    neverapplied 0.1.0: golang-migrate
      base examples/golang-migrate/base: 6 migration files, highest version 20251103120000
      head examples/golang-migrate/head: 8 migration files, 2 new, 1 changed, 0 removed

    ERROR NA101 examples/golang-migrate/head/20251020101500_add_users_email_unique.up.sql
      version 20251020101500 is below 20251103120000, the highest version on the base. golang-migrate stores only the latest version a database has applied, so a database already at 20251103120000 never runs this file, and reports no error.
      fix: rename it to 20251103120001_add_users_email_unique.up.sql so it sorts after 20251103120000, and its .down file to the same version

    ERROR NA102 examples/golang-migrate/head/20251014153000_create_orders.up.sql
      version 20251014153000 is on the base and its SQL changed. golang-migrate records only that a version ran, so a database that already applied 20251014153000 never runs the new SQL, and only new databases get it.
      fix: restore 20251014153000_create_orders.up.sql as it is on the base, and put the change in a new migration above 20251103120000

    ERROR NA105 examples/golang-migrate/head/20251104090000_backfill_order_status.sql
      the name doesn't match VERSION_TITLE.up.sql or VERSION_TITLE.down.sql, so golang-migrate ignores the file without an error.
      fix: rename it to the tool's pattern, with a version above 20251103120000

    Result: 3 errors, 0 warnings.
    neverapplied compares files only; it doesn't know which versions each database has applied.

`--fail-on never` is here only so the README's example exits 0. Without it, findings exit 1.

`examples/goose` adds one migration above the base's highest version and changes nothing else, so neverapplied stays quiet:

    $ go run ./plugin/scripts --base examples/goose/base examples/goose/head
    neverapplied 0.1.0: goose
      base examples/goose/base: 3 migration files, highest version 00003
      head examples/goose/head: 4 migration files, 1 new, 0 changed, 0 removed

    Result: every new migration sorts after 00003, and no SQL already on the base changed.
    neverapplied compares files only; it doesn't know which versions each database has applied.

`examples/dbmate` has a migration below main's latest, a second file with a version main already used, and a file missing its `-- migrate:down` line.

## Usage

    neverapplied (--base BASE_DIR | --base-ref REF) [--applied VERSION] [--tool auto|golang-migrate|goose|dbmate] [--format text|json] [--fail-on warn|error|never] HEAD_DIR

- `HEAD_DIR` is the migrations folder as it will be deployed: the pull request merged with main, which is what CI checks out for a pull request.
- `--base-ref` reads the same folder from a git revision such as `origin/main`, with read-only git commands. `--base` takes a second folder instead.
- `--applied` is the highest version any database has applied, for teams whose main branch holds migrations not released yet. Edits and removals above it are allowed, and new files are compared with it instead of the base's highest version.
- `--tool` is detected from file names and the `-- +goose Up` or `-- migrate:up` markers. Only the files directly in the folder are read, as the tools do.

| Exit code | Meaning |
|---|---|
| 0 | No findings at the `--fail-on` level, or `--fail-on never` |
| 1 | Findings at the `--fail-on` level: `warn` by default |
| 2 | Bad options |
| 3 | The input can't be trusted, or there is nothing to compare. No result is printed, whatever `--fail-on` says |

Exit 3 covers a missing folder, a file where a folder should be, an empty folder or one with no migrations for the tool, a git revision or folder that doesn't exist, and any migration file that is binary, not UTF-8, starts like the output of a failed command (git, make, go, goose, a shell), or looks cut off: empty, ending inside a string, comment, dollar-quoted body or open parenthesis, or ending on a comma, operator or a word such as `WHERE`. Files that are identical on both sides aren't checked for this, because databases have already run them. A missing final semicolon is refused for goose, which fails on it too, and for golang-migrate and dbmate only when every unchanged file in the folder ends with one. `go run` turns any failing exit code into 1, so build the binary when you need the exact code.

## Findings

| Code | Level | Meaning |
|---|---|---|
| NA101 | error | A new migration's version is below the highest version on the base (or `--applied`) |
| NA102 | error | A migration on the base changed its SQL, or its Go code for goose |
| NA103 | error | A migration was renamed to a new version, so it runs again, or golang-migrate's latest version file was removed |
| NA104 | error | Two files have the same version |
| NA105 | error | A `.sql` file the tool ignores or rejects because of its name. A warning if it is the same on the base |
| NA106 | error | A new or changed goose or dbmate file has no `-- +goose Up`, or no `-- migrate:up` or `-- migrate:down` line |
| NA202 | warn | Only the rollback SQL of a migration on the base changed |
| NA203 | warn | A migration on the base was removed |
| NA301 | note | Whitespace-only edit, or a rename that keeps the version |

## What each tool does

I ran each tool on a scratch SQLite database to check these. Versions: golang-migrate v4.20.1, goose v3.30.1, dbmate v2.38.2.

| Case | golang-migrate | goose | dbmate |
|---|---|---|---|
| New file below the latest applied version | Never run. `Up` returns `no change` | `goose up` fails: `found 1 missing migrations before current version 3` | Run after the newer ones. `--strict` refuses it |
| Edit to an applied migration | Never run | Never run | Never run |
| Two files with one version | Fails: `duplicate migration file` | Fails: `duplicate version 3 detected` | If the version is applied, the new file is skipped. On a new database: `UNIQUE constraint failed: schema_migrations.version` |
| Latest applied file removed | `Up` fails: `no migration found for version 2` | No error | No error, also with `--strict` |
| File name the tool doesn't parse | `2_b.sql` and `3-c.up.sql` are skipped with no error | `b.sql` fails: `no filename separator '_' found` | Names not starting with a digit are skipped |
| Missing marker or final semicolon | Not needed | No `-- +goose Up`: fails to parse. No final `;`: `missing semicolon?` | No `-- migrate:down`: fails |

## How it compares

[Atlas](https://atlasgo.io) detects out-of-order and edited migrations through its `atlas.sum` file, for teams that use Atlas to manage the folder. Flyway's `validate` refuses edited and out-of-order files at deploy time. [squawk](https://github.com/sbdchd/squawk) and similar linters check whether a migration's SQL is safe to run, one file at a time. golang-migrate, goose and dbmate users without Atlas have no check that compares the folder with what main already deployed, which is what neverapplied does, in a pull request, before anything runs.

## Limits

- **It doesn't know what each database has applied.** Without `--applied`, every version on the base counts as applied. A database that is behind main isn't affected by NA101 yet, and NA102 doesn't matter for a migration no database has run.
- **Changed means the text changed.** It doesn't parse SQL, so an edit that only changes a comment still counts. Whitespace-only edits are notes.
- **Cut-off detection is a heuristic.** A file cut at the end of a statement can't be told from a complete one. MySQL `DELIMITER` blocks and SQL Server `GO` batches aren't understood yet and may be refused as cut off.
- **Only one folder per run.** dbmate can read several folders; run neverapplied once per folder.
- **goose Go migrations** are compared as text and checked to parse. Their registration isn't followed.
- **Only golang-migrate, goose and dbmate.** Flyway folders are refused with a message, since Flyway's own `validate` covers this.

## FAQ

**Why didn't my golang-migrate migration run?** If its version is lower than the version the database is already at, golang-migrate skips it and reports `no change`. This happens when a branch is merged after a newer migration. Rename it above the current latest version.

**What does goose "found 1 missing migrations" mean?** A migration file has a version below the database's current version and hasn't been applied. goose refuses to continue unless you pass `-allow-missing`. Renaming the file above the latest version keeps every database in the same order.

**Can I edit a migration that was already merged?** Databases that ran it won't run it again, with any of these three tools. Write a new migration instead, unless no database has run the old one. Then pass `--applied` with the highest deployed version so neverapplied knows.

**Does it work with Postgres, MySQL and SQLite?** It reads file names and text, so the database doesn't matter for most checks. The cut-off check understands standard SQL quoting, Postgres dollar quotes, and MySQL backslash escapes and `#` comments.

**Is it an MCP server?** No. It's a skill with a Go package and a script that builds it, so Go is all it needs.

## Tested on

- Unit tests for each finding and each tool, version parsing (`01` and `1` clash in golang-migrate, dbmate compares versions as text), up and down sections, `--applied`, and the cut-off reader. Integration tests build the binary and run it on the three examples, on each side of each example in turn with eleven kinds of damage to its largest file (empty, whitespace, binary, random text, cut in half, cut after one line, cut before the last character, and output from failed go, make, git and goose commands), and on missing, empty, unrelated and file-instead-of-folder sides. Each must exit 3 with no result, with and without `--fail-on never`. `--base-ref` is tested on a real git repository, including a ref and a folder that don't exist and a cut-off file committed to the base.
- The tool behaviors in the table above, run by hand on scratch SQLite databases with the versions listed.
- Real history, comparing each commit that touched a migrations folder with its parent: goharbor/harbor `make/migrations/postgresql` (204 commits, golang-migrate), flipt-io/flipt `config/migrations` for postgres, mysql and sqlite3 (golang-migrate), and openfga/openfga `assets/migrations` (goose). Most flipt and openfga commits are quiet. Findings I checked by hand were real: harbor adds patch-release files such as `0181_2.15.3_schema.up.sql` below newer versions on main (NA101), and once renamed `0180_2.15.0` to `0171_2.14.1` (NA103). openfga changed `schema_version` from `INT` to `TEXT` in goose migration `002` after it was on main (NA102). flipt edited MySQL migration `12` and removed its down files (NA102, NA203).
- harbor edits its in-progress release file on main all the time, so 160 of its 204 commits get NA102. 98 of those came after a v2 release tag. With `--applied` set to the highest version in the most recent v2 release tag created before each commit, 97 of the 98 were quiet. The one left changed `0171_2.14.1_schema.up.sql` after it had shipped in a release tag.
- harbor also has files without a final semicolon. When a commit changed such a file while every unchanged file ended with one, the run was refused (9 of 204 commits).
- Not tested: running the tools on the real projects' databases, MySQL and SQL Server specific syntax beyond quoting, Windows, and use in a fresh Claude session.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/neverapplied/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes, and a small problem may become a follow-up issue instead. A PR with no new commit 14 days after a change request is closed, and you can reopen it. PRs that change CI, agent settings, npm or Python dependencies, vendored code or build files can't be merged this way, and a comment says so. The same goes for anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about two hours, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `neverapplied` saved you some prompts, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
