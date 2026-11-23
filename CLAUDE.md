# neverapplied

Claude Code skill and Go CLI that compares a branch's SQL migrations folder with the base branch and finds migrations that deployed databases will skip, refuse or run out of order, for golang-migrate, goose and dbmate.

## Architecture
All code is in `plugin/scripts`, one `package main` with no dependencies, so `go run` and `run.sh` need no setup. `input.go` reads a folder from disk or from a git revision, `migrations.go` parses file names the way each tool does, `trust.go` refuses input that can't be trusted, `check.go` compares the two sides, and `main.go` has flags and output. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet, unit tests and integration tests in `tests/`
- `make lint`: plugin and marketplace validation
- `make demo`: run the three examples

## Conventions
- Standard library only. Never connect to a database, and run only read-only git commands (`rev-parse`, `ls-tree`, `cat-file`).
- Input that can't be trusted (empty, binary, cut off, failed command output, no migrations, a missing ref) exits 3 with no result. Never print a clean result unless both sides were read and compared.
- A rule about what a tool does must come from that tool's source and a run on a scratch database, with the version noted in the pull request.
- Each finding has a stable `NAnnn` code. Don't reuse codes.
- Tests must not assume the machine they run on.

## Do and don't
- Do add a test for what a change flags, for what it must not flag, and for broken input on each side.
- Don't guess what a database has applied. The base, or `--applied`, is the only source.

## Definition of done
`make test`, `make lint` and `make demo` pass, and `README.md`, `ROADMAP.md` and `CHANGELOG.md` are updated when relevant.
