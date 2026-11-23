# 0001: A skill with Go source, not an MCP server

**Decision:** neverapplied ships as a Claude Code skill with a standard-library Go package and a `run.sh` that builds it, instead of an MCP server with prebuilt binaries.

**Why:** the agent needs one command and its output, which a skill gives without a server process. golang-migrate, goose and dbmate users mostly have Go already, and the same command runs in CI as a plain CLI.

**Alternatives:** an MCP server would need binaries for each platform and an npm package, for no extra capability. Linking the migration tools' own parsers would match them exactly, but adds dependencies and database drivers for what is a small amount of file name parsing.
