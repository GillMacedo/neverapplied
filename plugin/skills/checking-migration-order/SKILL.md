---
name: checking-migration-order
description: Checks a branch's SQL migrations against the base branch for files that databases already deployed will skip, refuse or run out of order, for golang-migrate, goose and dbmate. Finds a new migration whose version is below the highest version on main (golang-migrate silently never runs it, goose up fails with "found N missing migrations", dbmate runs it out of order), edits to migrations that already ran, removed or renamed versions, duplicate versions, and files the tool ignores because of their names. Use when reviewing a pull request that adds or changes migrations, after merging or rebasing a branch with migrations, or when a migration "didn't run" in staging or production.
---

# Checking migration order

Migration tools record which versions a database has applied, not what the files said. So a database deployed from main can skip a new file numbered below main's latest version, and never sees an edit to a file it already ran. This check compares the branch's migrations folder with the base and says what each tool will do.

## Run the tool

It needs Go 1.22 or newer and git for `--base-ref`. It only reads files and runs read-only git commands.

    sh "${CLAUDE_PLUGIN_ROOT}/scripts/run.sh" --base-ref origin/main db/migrations

- Pass the folder that holds the migration files, on the current checkout. `--base-ref` reads the same folder from a git revision. If the base branch isn't fetched, ask before running `git fetch`.
- `--base DIR` compares with a folder instead, such as an export of main.
- The tool is detected from file names and markers. Pass `--tool golang-migrate`, `--tool goose` or `--tool dbmate` if detection fails.
- If main has migrations that haven't been released yet, ask the user for the highest version deployed to any database and pass `--applied VERSION`. Edits and removals above it are then allowed.
- Add `--format json` to read the result in detail.

Exit codes: 0 means no problems, 1 means findings, 2 means bad options, and 3 means the input can't be trusted (empty, binary, cut off, the output of a failed command, a missing git ref, or no migrations found). With exit 3 there is no result, so never report the migrations as fine. Read the message and fix the input.

## Read the result

- `NA101`: a new migration sorts below the base's highest version. With golang-migrate it is never run on existing databases, with no error. With goose, `goose up` fails. With dbmate it runs out of order. Rename it above the highest version.
- `NA102`: a migration already on the base changed. Existing databases keep the old version. Restore it and write a new migration, unless the user confirms no database has run it (then rerun with `--applied`).
- `NA103`: a version was renamed, so its SQL will run again, or golang-migrate's current version file was removed.
- `NA104`: two files share a version.
- `NA105`: a file the tool ignores or rejects because of its name.
- `NA106`: a goose or dbmate file missing its `-- +goose Up` or `-- migrate:up` / `-- migrate:down` lines.
- `NA202` and `NA203` are warnings: rollback SQL changed, or a version was removed. `NA301` is a note.

## Report

1. For each error, give the file, what the tool will do on an existing database, and the fix the tool suggests.
2. Propose the smallest change, such as a rename to the suggested version. For golang-migrate, rename the `.up` and `.down` files together.
3. Say what wasn't checked: the versions each real database has applied, SQL correctness, and whether the migration is safe to run on a large table.

## Don't

Don't rename or edit migration files unless the user asks. Don't call the migrations safe when the tool exited 3.
