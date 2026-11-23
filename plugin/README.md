# neverapplied plugin

This plugin adds the `checking-migration-order` skill. It compares a branch's SQL migrations folder with the same folder on the base branch and finds migrations that databases already deployed will skip, refuse or run out of order, for golang-migrate, goose and dbmate. It reads files only and never connects to a database.

It needs Go 1.22 or newer: `scripts/run.sh` builds the tool from the bundled source each time it runs. See the repository README for usage, findings and limits.
