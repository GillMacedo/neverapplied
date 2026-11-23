# Architecture

    head folder (disk)            --> input.go: regular files directly in the folder
    base folder (disk) or git ref --> input.go: same, or git ls-tree + git cat-file --batch for REF:folder
    migrations.go --> detect the tool, parse names the way it does, key each file by what the tool records
                      (version and direction for golang-migrate, version for goose and dbmate)
    trust.go      --> refuse binary, non-UTF-8 and failed-command text everywhere; refuse empty or cut-off
                      SQL and unparseable Go in any file that isn't byte-identical on both sides
    check.go      --> compare by key: new below the highest applied version, changed, removed, renamed,
                      duplicate, misnamed, missing markers
    main.go       --> flags, text or JSON, exit code

- **What a database has applied** is assumed to be every version on the base, up to its highest. `--applied` lowers that line for teams whose main branch holds migrations not deployed yet.
- **Per-tool behavior** comes from each tool's source and runs on scratch SQLite databases: golang-migrate v4.20.1, goose v3.30.1 and dbmate v2.38.2. The README lists what was run.
- **Changed means the SQL changed.** Whitespace-only edits are a note. For goose and dbmate, which keep up and down in one file, an edit only to the down section is a warning.
- **Cut-off detection** reads SQL for open strings, comments, dollar quotes and parentheses, and for a statement ending on a comma, operator or keyword such as `WHERE`. It also reads it with MySQL's backslash escapes and `#` comments, and accepts either reading. A missing final semicolon is refused for goose, which rejects it too, and for golang-migrate and dbmate only when every unchanged file ends with one.

Key choices are in `decisions/`.
