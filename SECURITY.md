# Security

Report vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/GillMacedo/neverapplied/security/advisories/new), not in a public issue.

`neverapplied` only reads files and runs read-only git commands. It never connects to a database or the network. A wrong answer is a normal bug, unless it reports migrations as fine when a database would skip or refuse one, in a way that could hide data loss or a failed deploy. If it could, you may report it privately.
