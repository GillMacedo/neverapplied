// Command neverapplied compares a migrations folder on a branch with the
// same folder on the base branch and reports SQL migrations that databases
// already deployed from the base will skip, refuse or run out of order:
// new files below the highest applied version, edits to migrations that
// already ran, removed or renamed versions, duplicate versions, and files
// the tool ignores because of their names. It supports golang-migrate,
// goose and dbmate, and never connects to a database.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const version = "0.1.0"

// Exit codes.
const (
	exitOK       = 0
	exitFindings = 1
	exitUsage    = 2
	exitInput    = 3
)

// inputError means the input can't be trusted, so no result is printed.
type inputError struct{ msg string }

func (e inputError) Error() string { return e.msg }

func untrusted(format string, a ...any) error { return inputError{fmt.Sprintf(format, a...)} }

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usageErr(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

// cleanErr drops the operation prefix from path errors, which already
// name the path in the message around them.
func cleanErr(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

type options struct {
	base    string
	baseRef string
	tool    string
	format  string
	failOn  string
	head    string
	applied string
}

func parseArgs(args []string, stderr io.Writer) (*options, bool, error) {
	fl := flag.NewFlagSet("neverapplied", flag.ContinueOnError)
	fl.SetOutput(stderr)
	var o options
	showVersion := false
	fl.StringVar(&o.base, "base", "", "the migrations folder as it is on the base branch")
	fl.StringVar(&o.baseRef, "base-ref", "", "read the base from this git revision instead, such as origin/main")
	fl.StringVar(&o.applied, "applied", "", "highest version any database has applied, when the base has migrations not deployed yet")
	fl.StringVar(&o.tool, "tool", "auto", "migration tool: golang-migrate, goose, dbmate or auto")
	fl.StringVar(&o.format, "format", "text", "output format: text or json")
	fl.StringVar(&o.failOn, "fail-on", "warn", "exit 1 on findings at this level or worse: error, warn or never")
	fl.BoolVar(&showVersion, "version", false, "print the version")
	fl.Usage = func() {
		fmt.Fprint(stderr, `usage: neverapplied (--base BASE_DIR | --base-ref REF) [flags] HEAD_DIR

Compares the migrations in HEAD_DIR with the base, and reports migrations
that databases already deployed from the base will skip, refuse or run out
of order. Supports golang-migrate, goose and dbmate. Reads files only.

flags:
`)
		fl.PrintDefaults()
	}
	// Allow flags after the folder, as in "neverapplied db/migrations --base-ref main".
	var rest []string
	for len(args) > 0 {
		if err := fl.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, true, nil
			}
			return nil, false, usageErr("%s", err)
		}
		args = fl.Args()
		if len(args) > 0 {
			rest = append(rest, args[0])
			args = args[1:]
		}
	}
	if showVersion {
		fmt.Fprintln(os.Stdout, "neverapplied", version)
		return nil, true, nil
	}
	if len(rest) != 1 {
		return nil, false, usageErr("give exactly one head migrations folder")
	}
	o.head = rest[0]
	if (o.base == "") == (o.baseRef == "") {
		return nil, false, usageErr("give either --base BASE_DIR or --base-ref REF")
	}
	switch o.tool {
	case "auto", toolMigrate, toolGoose, toolDbmate:
	case "migrate":
		o.tool = toolMigrate
	default:
		return nil, false, usageErr("--tool must be golang-migrate, goose, dbmate or auto, not %q", o.tool)
	}
	if o.format != "text" && o.format != "json" {
		return nil, false, usageErr("--format must be text or json")
	}
	switch o.failOn {
	case levelError, levelWarn, "never":
	default:
		return nil, false, usageErr("--fail-on must be error, warn or never")
	}
	if o.base != "" {
		a, errA := filepath.Abs(o.base)
		b, errB := filepath.Abs(o.head)
		if errA == nil && errB == nil && a == b {
			return nil, false, usageErr("--base and the head folder are the same folder, so there is nothing to compare")
		}
	}
	return &o, false, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	o, done, err := parseArgs(args, stderr)
	if done {
		return exitOK
	}
	if err != nil {
		fmt.Fprintln(stderr, "neverapplied:", err)
		return exitUsage
	}
	r, err := execute(o)
	if err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintln(stderr, "neverapplied:", err)
			return exitUsage
		}
		fmt.Fprintln(stderr, "neverapplied: no result:", err)
		return exitInput
	}
	if o.format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(r)
	} else {
		printText(stdout, r)
	}
	if fails(r, o.failOn) {
		return exitFindings
	}
	return exitOK
}

func execute(o *options) (*report, error) {
	head, err := readDir(o.head)
	if err != nil {
		return nil, err
	}
	var base side
	if o.baseRef != "" {
		base, err = readGit(o.head, o.baseRef)
	} else {
		base, err = readDir(o.base)
	}
	if err != nil {
		return nil, err
	}
	tool := o.tool
	if tool == "auto" {
		var names []string
		tool, names = detectTool(base, head)
		if tool == "" {
			switch {
			case len(names) == 1 && names[0] == "flyway":
				return nil, untrusted("these look like Flyway migrations, which neverapplied doesn't support. Flyway's validate step already refuses edited and out-of-order migrations")
			case len(names) > 1:
				return nil, untrusted("the files match more than one tool (%s). Pass --tool", strings.Join(names, ", "))
			default:
				return nil, untrusted("no golang-migrate, goose or dbmate migrations found in %s or %s. Pass the folder that holds them, or --tool", head.Label, base.Label)
			}
		}
	}
	return check(tool, base, head, o.applied)
}

func fails(r *report, failOn string) bool {
	for _, f := range r.Findings {
		switch failOn {
		case levelError:
			if f.Level == levelError {
				return true
			}
		case levelWarn:
			if f.Level == levelError || f.Level == levelWarn {
				return true
			}
		}
	}
	return false
}

func printText(w io.Writer, r *report) {
	fmt.Fprintf(w, "neverapplied %s: %s\n", version, r.Tool)
	fmt.Fprintf(w, "  base %s: %d migration files, highest version %s\n", r.Base, r.BaseCount, r.BaseHighest)
	if r.Applied != "" {
		fmt.Fprintf(w, "  applied: up to version %s (--applied)\n", r.Applied)
	}
	fmt.Fprintf(w, "  head %s: %d migration files, %d new, %d changed, %d removed\n", r.Head, r.HeadCount, r.New, r.Changed, r.Removed)
	counts := map[string]int{}
	for _, f := range r.Findings {
		counts[f.Level]++
		fmt.Fprintf(w, "\n%s %s %s\n", strings.ToUpper(f.Level), f.Code, f.Path)
		fmt.Fprintf(w, "  %s\n", f.Message)
		if f.Fix != "" {
			fmt.Fprintf(w, "  fix: %s\n", f.Fix)
		}
	}
	fmt.Fprintln(w)
	switch {
	case counts[levelError]+counts[levelWarn] > 0:
		fmt.Fprintf(w, "Result: %s, %s.\n", plural(counts[levelError], "error"), plural(counts[levelWarn], "warning"))
	case r.New+r.Changed+r.Removed == 0:
		fmt.Fprintln(w, "Result: no new, changed or removed migrations.")
	default:
		fmt.Fprintf(w, "Result: every new migration sorts after %s, and no SQL already on the base changed.\n", r.BaseHighest)
	}
	fmt.Fprintln(w, "neverapplied compares files only; it doesn't know which versions each database has applied.")
}

func plural(n int, s string) string {
	if n == 1 {
		return "1 " + s
	}
	return fmt.Sprintf("%d %ss", n, s)
}
