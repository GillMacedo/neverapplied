# 0003: The base branch stands for what databases have applied

**Decision:** without `--applied`, every version on the base counts as applied by some database.

**Why:** neverapplied never connects to a database, and in most teams main is what gets deployed. This makes the check work in a pull request with nothing but git.

**Trade-off:** teams that edit a migration on main before releasing it, as harbor does with its per-release schema files, get NA102 on every such edit. `--applied VERSION` fixes that: on 98 harbor commits with an earlier release tag, passing that release's highest version left one finding, which was an edit to a released migration.
