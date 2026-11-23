# Changelog

All notable changes are documented here, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-23

### Added
- Compare a migrations folder with its base, from a folder (`--base`) or a git revision (`--base-ref`), for golang-migrate, goose and dbmate.
- Findings for new migrations below the base's highest version, edits to migrations on the base, removed and renamed versions, duplicate versions, misnamed files, and goose or dbmate files missing their markers.
- `--applied` for bases with migrations not deployed yet, `--format json` and `--fail-on`.
- Exit code 3, with no result, for empty, binary, cut-off or failed-command input, missing folders or refs, and folders with no migrations.
- `checking-migration-order` skill and plugin manifest.
