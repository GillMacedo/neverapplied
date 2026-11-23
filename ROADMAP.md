# Roadmap

## Next
- `sql-migrate` (rubenv) and `tern`, after checking their behavior on a scratch database
- A GitHub Actions example that fetches the base branch and runs `--base-ref`
- Read `--applied` from a file, such as the output of `migrate version` or `goose version` saved by a deploy job

## Later
- goose's `-- +goose NO TRANSACTION` and other annotations that change how a file runs
- MySQL `DELIMITER` blocks, which the cut-off check doesn't understand yet
- Flyway, whose `validate` already refuses edited and out-of-order files, only if users ask for it
