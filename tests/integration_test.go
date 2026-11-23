// Package tests builds neverapplied and runs it the way users do: on the
// examples, on broken input on each side with the other side real, and
// against a real git repository with --base-ref.
package tests

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "neverapplied-test")
	if err != nil {
		panic(err)
	}
	name := "neverapplied"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary = filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", binary, "./plugin/scripts")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		panic("go build: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// run runs neverapplied in dir, or the repo root when dir is "".
func run(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = ".."
	if dir != "" {
		cmd.Dir = dir
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

func TestExamples(t *testing.T) {
	out, _, code := run(t, "", "--base", "examples/golang-migrate/base", "examples/golang-migrate/head")
	for _, want := range []string{"ERROR NA101", "ERROR NA102", "ERROR NA105", "20251103120001_add_users_email_unique.up.sql", "Result: 3 errors, 0 warnings."} {
		if !strings.Contains(out, want) {
			t.Errorf("golang-migrate: missing %q\n%s", want, out)
		}
	}
	if code != 1 {
		t.Errorf("golang-migrate: exit %d", code)
	}
	if _, _, code := run(t, "", "--fail-on", "never", "--base", "examples/golang-migrate/base", "examples/golang-migrate/head"); code != 0 {
		t.Errorf("golang-migrate --fail-on never: exit %d", code)
	}
	if _, _, code := run(t, "", "--fail-on", "error", "--base", "examples/golang-migrate/base", "examples/golang-migrate/head"); code != 1 {
		t.Errorf("golang-migrate --fail-on error: exit %d", code)
	}

	out, _, code = run(t, "", "--base", "examples/dbmate/base", "examples/dbmate/head")
	for _, want := range []string{"ERROR NA101", "ERROR NA104", "ERROR NA106", "dbmate --strict refuses it"} {
		if !strings.Contains(out, want) {
			t.Errorf("dbmate: missing %q\n%s", want, out)
		}
	}
	if code != 1 {
		t.Errorf("dbmate: exit %d", code)
	}

	// The healthy example must stay quiet and exit 0.
	out, _, code = run(t, "", "--base", "examples/goose/base", "examples/goose/head")
	if code != 0 || strings.Contains(out, "ERROR") || strings.Contains(out, "WARN") || !strings.Contains(out, "1 new, 0 changed, 0 removed") {
		t.Errorf("goose: exit %d\n%s", code, out)
	}
	// Flags after the folder work too.
	if _, _, code := run(t, "", "examples/goose/head", "--base", "examples/goose/base", "--tool", "goose"); code != 0 {
		t.Errorf("goose, flags last: exit %d", code)
	}
}

func TestJSON(t *testing.T) {
	out, _, code := run(t, "", "--format", "json", "--base", "examples/golang-migrate/base", "examples/golang-migrate/head")
	if code != 1 || !strings.Contains(out, `"code": "NA101"`) || !strings.Contains(out, `"baseHighestVersion": "20251103120000"`) {
		t.Errorf("exit %d\n%s", code, out)
	}
}

// damaged returns versions of real that must never give a result.
func damaged(t *testing.T, real []byte) map[string][]byte {
	junk := make([]byte, 512)
	if _, err := rand.Read(junk); err != nil {
		t.Fatal(err)
	}
	junk[0] = 0
	trimmed := bytes.TrimRight(real, "\n")
	return map[string][]byte{
		"empty":        {},
		"whitespace":   []byte("\n\n  \n"),
		"binary":       junk,
		"text":         []byte("lorem ipsum dolor sit amet\n"),
		"cut-half":     real[:len(real)/2],
		"cut-line":     real[:bytes.IndexByte(real, '\n')+1],
		"cut-end":      trimmed[:len(trimmed)-1],
		"failed-go":    []byte("# example.com/app\n./main.go:14:2: undefined: muxx\nFAIL\texample.com/app [build failed]\n"),
		"failed-make":  []byte("make: *** [Makefile:12: migrations] Error 1\n"),
		"failed-git":   []byte("fatal: path 'db/migrations/0001_init.up.sql' does not exist in 'origin/main'\n"),
		"failed-goose": []byte("goose run: ERROR 00002_create_invoices.sql: failed to run SQL migration\n"),
	}
}

// largest returns the name of the biggest migration file in dir, the one
// with the most room to be cut.
func largest(t *testing.T, dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	type fe struct {
		name string
		size int64
	}
	var fs []fe
	for _, e := range entries {
		info, _ := e.Info()
		fs = append(fs, fe{e.Name(), info.Size()})
	}
	sort.Slice(fs, func(i, j int) bool {
		return fs[i].size > fs[j].size || fs[i].size == fs[j].size && fs[i].name < fs[j].name
	})
	return fs[0].name
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func refused(t *testing.T, what string, dir string, args ...string) {
	t.Helper()
	for _, extra := range [][]string{nil, {"--fail-on", "never"}, {"--fail-on", "error"}, {"--format", "json"}} {
		a := append(append([]string{}, extra...), args...)
		out, errs, code := run(t, dir, a...)
		if code != 3 || out != "" || !strings.Contains(errs, "no result") {
			t.Errorf("%s %v: exit %d, stdout %q, stderr %q", what, extra, code, out, errs)
		}
	}
}

// Every example, each side in turn, with each kind of damage to its
// largest migration file, while the other side stays real.
func TestBrokenFiles(t *testing.T) {
	for _, ex := range []string{"golang-migrate", "goose", "dbmate"} {
		for _, broken := range []string{"base", "head"} {
			src := filepath.Join("..", "examples", ex, broken)
			name := largest(t, src)
			real, err := os.ReadFile(filepath.Join(src, name))
			if err != nil {
				t.Fatal(err)
			}
			for kind, data := range damaged(t, real) {
				t.Run(ex+"/"+broken+"/"+kind, func(t *testing.T) {
					tmp := t.TempDir()
					copyDir(t, filepath.Join("..", "examples", ex, "base"), filepath.Join(tmp, "base"))
					copyDir(t, filepath.Join("..", "examples", ex, "head"), filepath.Join(tmp, "head"))
					if err := os.WriteFile(filepath.Join(tmp, broken, name), data, 0o644); err != nil {
						t.Fatal(err)
					}
					refused(t, name, "", "--base", filepath.Join(tmp, "base"), filepath.Join(tmp, "head"))
				})
			}
		}
	}
}

func TestBrokenFolders(t *testing.T) {
	for _, broken := range []string{"base", "head"} {
		for _, kind := range []string{"empty", "missing", "file", "unrelated"} {
			t.Run(broken+"/"+kind, func(t *testing.T) {
				tmp := t.TempDir()
				copyDir(t, "../examples/golang-migrate/base", filepath.Join(tmp, "base"))
				copyDir(t, "../examples/golang-migrate/head", filepath.Join(tmp, "head"))
				p := filepath.Join(tmp, broken)
				os.RemoveAll(p)
				switch kind {
				case "empty":
					os.Mkdir(p, 0o755)
				case "file":
					// What a failed command leaves when its output is redirected.
					os.WriteFile(p, []byte("fatal: not a git repository (or any of the parent directories): .git\n"), 0o644)
				case "unrelated":
					os.Mkdir(p, 0o755)
					os.WriteFile(filepath.Join(p, "README.md"), []byte("# Migrations\n"), 0o644)
					os.WriteFile(filepath.Join(p, "schema.sql"), []byte("CREATE TABLE a (id int);\n"), 0o644)
				}
				refused(t, kind, "", "--base", filepath.Join(tmp, "base"), filepath.Join(tmp, "head"))
				refused(t, kind+" with --tool", "", "--tool", "golang-migrate", "--base", filepath.Join(tmp, "base"), filepath.Join(tmp, "head"))
			})
		}
	}
	// A Flyway folder is refused, not passed as clean.
	tmp := t.TempDir()
	for _, s := range []string{"base", "head"} {
		os.Mkdir(filepath.Join(tmp, s), 0o755)
		os.WriteFile(filepath.Join(tmp, s, "V1__init.sql"), []byte("CREATE TABLE a (id int);\n"), 0o644)
	}
	refused(t, "flyway", "", "--base", filepath.Join(tmp, "base"), filepath.Join(tmp, "head"))
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// --base-ref reads the base from a real git repository and must give the
// same result as two folders.
func TestBaseRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	mig := filepath.Join(repo, "db", "migrations")
	copyDir(t, "../examples/golang-migrate/base", mig)
	gitCmd(t, repo, "init", "-q")
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-q", "-m", "base")

	// Nothing changed yet: a real comparison with nothing new.
	out, errs, code := run(t, mig, "--base-ref", "HEAD", ".")
	if code != 0 || !strings.Contains(out, "no new, changed or removed migrations") || !strings.Contains(out, "HEAD:db/migrations") {
		t.Errorf("unchanged: exit %d\n%s%s", code, out, errs)
	}

	for _, e := range []string{"20251001090000_create_users.up.sql", "20251014153000_create_orders.up.sql"} {
		os.Remove(filepath.Join(mig, e))
	}
	copyDir(t, "../examples/golang-migrate/head", mig)
	want, _, _ := run(t, "", "--format", "json", "--base", "examples/golang-migrate/base", "examples/golang-migrate/head")
	got, errs, code := run(t, repo, "--format", "json", "--base-ref", "HEAD", "db/migrations")
	if code != 1 {
		t.Fatalf("branch: exit %d %s", code, errs)
	}
	strip := func(s string) string {
		var keep []string
		for _, l := range strings.Split(s, "\n") {
			if strings.Contains(l, `"code"`) || strings.Contains(l, `"message"`) || strings.Contains(l, `"fix"`) || strings.Contains(l, `"new"`) || strings.Contains(l, `"changed"`) {
				keep = append(keep, strings.TrimSpace(l))
			}
		}
		return strings.Join(keep, "\n")
	}
	if strip(got) != strip(want) {
		t.Errorf("--base-ref and --base differ:\n%s\n---\n%s", strip(got), strip(want))
	}

	// A ref that doesn't exist, a folder the ref doesn't have, and a
	// folder outside any repository are refused.
	refused(t, "missing ref", repo, "--base-ref", "origin/main", "db/migrations")
	os.MkdirAll(filepath.Join(repo, "db", "other"), 0o755)
	copyDir(t, "../examples/golang-migrate/head", filepath.Join(repo, "db", "other"))
	refused(t, "folder not in ref", repo, "--base-ref", "HEAD", "db/other")
	outside := t.TempDir()
	copyDir(t, "../examples/golang-migrate/head", outside)
	refused(t, "not a repository", outside, "--base-ref", "HEAD", ".")
	// A broken file on the base side, committed to git.
	repo2 := t.TempDir()
	mig2 := filepath.Join(repo2, "m")
	copyDir(t, "../examples/goose/base", mig2)
	os.WriteFile(filepath.Join(mig2, "00002_create_invoices.sql"), []byte("-- +goose Up\nCREATE TABLE invoices (\n    id BIGSERIAL"), 0o644)
	gitCmd(t, repo2, "init", "-q")
	gitCmd(t, repo2, "add", ".")
	gitCmd(t, repo2, "commit", "-q", "-m", "base")
	copyDir(t, "../examples/goose/head", mig2)
	refused(t, "cut file in git base", repo2, "--base-ref", "HEAD", "m")
}

func TestRunScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("run.sh needs a POSIX shell")
	}
	cmd := exec.Command("sh", "plugin/scripts/run.sh", "--base", "examples/goose/base", "examples/goose/head")
	cmd.Dir = ".."
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "every new migration sorts after 00003") {
		t.Errorf("run.sh: %v\n%s", err, out)
	}
	cmd = exec.Command("sh", "plugin/scripts/run.sh", "--base", "examples/dbmate/base", "examples/dbmate/head")
	cmd.Dir = ".."
	err = cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Errorf("run.sh with findings: %v", err)
	}
}
