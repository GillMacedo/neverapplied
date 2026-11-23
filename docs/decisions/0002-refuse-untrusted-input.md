# 0002: Refuse input that can't be trusted, with exit code 3

**Decision:** when either side is empty, missing, a file instead of a folder, has no migrations, or holds a migration that is binary, not UTF-8, starts like a failed command's output, or looks cut off, neverapplied prints no result and exits 3, whatever `--fail-on` says.

**Why:** a CI check that passes on an empty checkout, a failed `git fetch` or a half-written file hides the problem it exists to find. Comparing two sides makes this easy to get wrong: a truncated base file only shows up as "changed", which `--fail-on never` would let through.

**Trade-off:** files that are byte-identical on both sides aren't checked for completeness, because databases already ran them. Some real projects leave out the final semicolon, which golang-migrate and dbmate accept, so a missing one is only refused when every unchanged file in the folder has one. A file cut exactly at the end of a statement can't be told from a complete one.
