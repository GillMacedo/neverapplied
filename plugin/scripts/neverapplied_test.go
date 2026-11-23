package main

import (
	"bytes"
	"strings"
	"testing"
)

func mk(label string, files ...string) side {
	s := side{Label: label}
	for i := 0; i+1 < len(files); i += 2 {
		s.Files = append(s.Files, file{Name: files[i], Path: label + "/" + files[i], Content: []byte(files[i+1])})
	}
	return s
}

func codes(r *report) string {
	var c []string
	for _, f := range r.Findings {
		c = append(c, f.Code)
	}
	return strings.Join(c, " ")
}

func mustCheck(t *testing.T, tool string, base, head side) *report {
	t.Helper()
	r, err := check(tool, base, head, "")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return r
}

func mustRefuse(t *testing.T, tool string, base, head side, want string) {
	t.Helper()
	r, err := check(tool, base, head, "")
	if err == nil {
		t.Fatalf("expected input error containing %q, got findings %q", want, codes(r))
	}
	if _, ok := err.(inputError); !ok {
		t.Fatalf("expected an inputError, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q doesn't contain %q", err, want)
	}
}

const (
	upA = "CREATE TABLE a (id int);\n"
	upB = "CREATE TABLE b (id int);\n"
	upC = "CREATE TABLE c (id int);\n"
)

func TestMigrateOutOfOrder(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA, "1_a.down.sql", "DROP TABLE a;\n", "3_c.up.sql", upC)
	head := mk("h", "1_a.up.sql", upA, "1_a.down.sql", "DROP TABLE a;\n", "2_b.up.sql", upB, "2_b.down.sql", "", "3_c.up.sql", upC)
	r := mustCheck(t, toolMigrate, base, head)
	if codes(r) != "NA101" {
		t.Fatalf("codes = %q", codes(r))
	}
	f := r.Findings[0]
	if f.Version != "2" || !strings.Contains(f.Message, "never runs this file") || !strings.Contains(f.Fix, "4_b.up.sql") {
		t.Errorf("finding = %+v", f)
	}
	if r.New != 2 || r.BaseHighest != "3" {
		t.Errorf("new=%d highest=%s", r.New, r.BaseHighest)
	}
}

func TestMigrateNewAboveIsQuiet(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB)
	head := mk("h", "1_a.up.sql", upA, "2_b.up.sql", upB, "3_c.up.sql", upC, "3_c.down.sql", "DROP TABLE c;\n", "README.md", "notes\n")
	r := mustCheck(t, toolMigrate, base, head)
	if len(r.Findings) != 0 || r.New != 2 {
		t.Fatalf("findings %q new %d", codes(r), r.New)
	}
}

func TestIdenticalIsQuiet(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB)
	r := mustCheck(t, toolMigrate, base, mk("h", "1_a.up.sql", upA, "2_b.up.sql", upB))
	if len(r.Findings) != 0 || r.New+r.Changed+r.Removed != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestEditedMigration(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB)
	head := mk("h", "1_a.up.sql", "CREATE TABLE a (id int, name text);\n", "2_b.up.sql", upB)
	r := mustCheck(t, toolMigrate, base, head)
	if codes(r) != "NA102" || r.Changed != 1 {
		t.Fatalf("codes %q changed %d", codes(r), r.Changed)
	}
}

func TestWhitespaceOnlyEditIsNote(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB)
	head := mk("h", "1_a.up.sql", "CREATE TABLE a (id int);\n\n", "2_b.up.sql", upB)
	r := mustCheck(t, toolMigrate, base, head)
	if codes(r) != "NA301" || fails(r, levelWarn) {
		t.Fatalf("codes %q", codes(r))
	}
}

func TestDownEditIsWarning(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA, "1_a.down.sql", "DROP TABLE a;\n")
	head := mk("h", "1_a.up.sql", upA, "1_a.down.sql", "DROP TABLE IF EXISTS a;\n")
	r := mustCheck(t, toolMigrate, base, head)
	if codes(r) != "NA202" || !fails(r, levelWarn) || fails(r, levelError) {
		t.Fatalf("codes %q", codes(r))
	}
}

func TestGooseDownSectionEditIsWarning(t *testing.T) {
	base := mk("b", "001_a.sql", "-- +goose Up\n"+upA+"-- +goose Down\nDROP TABLE a;\n")
	head := mk("h", "001_a.sql", "-- +goose Up\n"+upA+"-- +goose Down\nDROP TABLE IF EXISTS a;\n")
	r := mustCheck(t, toolGoose, base, head)
	if codes(r) != "NA202" {
		t.Fatalf("codes %q", codes(r))
	}
	head = mk("h", "001_a.sql", "-- +goose Up\nCREATE TABLE a (id bigint);\n-- +goose Down\nDROP TABLE a;\n")
	if r := mustCheck(t, toolGoose, base, head); codes(r) != "NA102" {
		t.Fatalf("codes %q", codes(r))
	}
}

func TestRemovedLatestMigrateIsError(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB)
	r := mustCheck(t, toolMigrate, base, mk("h", "1_a.up.sql", upA))
	if codes(r) != "NA103" || !strings.Contains(r.Findings[0].Message, "no migration found for version 2") {
		t.Fatalf("%q %+v", codes(r), r.Findings)
	}
	// Removing an older one is a warning.
	base = mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB, "3_c.up.sql", upC)
	r = mustCheck(t, toolMigrate, base, mk("h", "1_a.up.sql", upA, "3_c.up.sql", upC))
	if codes(r) != "NA203" {
		t.Fatalf("%q", codes(r))
	}
}

func TestRenamedToNewVersion(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB)
	head := mk("h", "1_a.up.sql", upA, "5_b.up.sql", upB)
	r := mustCheck(t, toolMigrate, base, head)
	if codes(r) != "NA103" || !strings.Contains(r.Findings[0].Message, "runs the same SQL again") {
		t.Fatalf("%q %+v", codes(r), r.Findings)
	}
}

func TestRenamedSameVersionIsNote(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB)
	head := mk("h", "1_a.up.sql", upA, "2_bee.up.sql", upB)
	r := mustCheck(t, toolMigrate, base, head)
	if codes(r) != "NA301" {
		t.Fatalf("%q", codes(r))
	}
}

func TestDuplicates(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA)
	head := mk("h", "1_a.up.sql", upA, "2_b.up.sql", upB, "2_c.up.sql", upC)
	r := mustCheck(t, toolMigrate, base, head)
	if codes(r) != "NA104" || !strings.Contains(r.Findings[0].Message, "duplicate migration file") {
		t.Fatalf("%q", codes(r))
	}
	// golang-migrate parses versions as numbers, so 01 and 1 clash.
	head = mk("h", "1_a.up.sql", upA, "01_b.up.sql", upB)
	if r := mustCheck(t, toolMigrate, base, head); !strings.Contains(codes(r), "NA104") {
		t.Fatalf("%q", codes(r))
	}
	// Up and down with the same version are a pair, not duplicates.
	head = mk("h", "1_a.up.sql", upA, "2_b.up.sql", upB, "2_b.down.sql", "DROP TABLE b;\n")
	if r := mustCheck(t, toolMigrate, base, head); len(r.Findings) != 0 {
		t.Fatalf("%q", codes(r))
	}
}

func TestDbmateDuplicateOfApplied(t *testing.T) {
	m := func(sql string) string { return "-- migrate:up\n" + sql + "-- migrate:down\n" }
	base := mk("b", "20250101_a.sql", m(upA))
	head := mk("h", "20250101_a.sql", m(upA), "20250101_b.sql", m(upB))
	r := mustCheck(t, toolDbmate, base, head)
	if codes(r) != "NA104" || r.Findings[0].Path != "h/20250101_b.sql" || r.New != 1 {
		t.Fatalf("%q %+v new=%d", codes(r), r.Findings, r.New)
	}
}

func TestMisnamed(t *testing.T) {
	base := mk("b", "1_a.up.sql", upA)
	head := mk("h", "1_a.up.sql", upA, "2_b.sql", upB, "3-c.up.sql", upC)
	r := mustCheck(t, toolMigrate, base, head)
	if codes(r) != "NA105 NA105" {
		t.Fatalf("%q", codes(r))
	}
	// The same misnamed file on the base is a warning.
	base = mk("b", "1_a.up.sql", upA, "2_b.sql", upB)
	head = mk("h", "1_a.up.sql", upA, "2_b.sql", upB)
	r = mustCheck(t, toolMigrate, base, head)
	if codes(r) != "NA105" || r.Findings[0].Level != levelWarn {
		t.Fatalf("%q", codes(r))
	}
	gb := mk("b", "001_a.sql", "-- +goose Up\n"+upA)
	gh := mk("h", "001_a.sql", "-- +goose Up\n"+upA, "b.sql", "-- +goose Up\n"+upB)
	if r := mustCheck(t, toolGoose, gb, gh); codes(r) != "NA105" || !strings.Contains(r.Findings[0].Message, "no filename separator") {
		t.Fatalf("%q", codes(r))
	}
}

func TestMarkers(t *testing.T) {
	gb := mk("b", "001_a.sql", "-- +goose Up\n"+upA)
	gh := mk("h", "001_a.sql", "-- +goose Up\n"+upA, "002_b.sql", upB)
	if r := mustCheck(t, toolGoose, gb, gh); codes(r) != "NA106" {
		t.Fatalf("%q", codes(r))
	}
	db := mk("b", "1_a.sql", "-- migrate:up\n"+upA+"-- migrate:down\n")
	dh := mk("h", "1_a.sql", "-- migrate:up\n"+upA+"-- migrate:down\n", "2_b.sql", "-- migrate:up\n"+upB)
	if r := mustCheck(t, toolDbmate, db, dh); codes(r) != "NA106" || !strings.Contains(r.Findings[0].Message, "migrate:down") {
		t.Fatalf("%q", codes(r))
	}
}

func TestGooseGoMigrations(t *testing.T) {
	goFile := "package migrations\n\nimport \"github.com/pressly/goose/v3\"\n\nfunc init() { goose.AddMigrationContext(up, down) }\n"
	base := mk("b", "001_a.sql", "-- +goose Up\n"+upA, "003_c.go", goFile)
	head := mk("h", "001_a.sql", "-- +goose Up\n"+upA, "002_b.go", goFile, "003_c.go", goFile, "helpers.go", "package migrations\n")
	r := mustCheck(t, toolGoose, base, head)
	if codes(r) != "NA101" {
		t.Fatalf("%q", codes(r))
	}
	// A cut-off Go migration is refused.
	head = mk("h", "001_a.sql", "-- +goose Up\n"+upA, "003_c.go", goFile, "004_d.go", goFile[:40])
	mustRefuse(t, toolGoose, base, head, "doesn't parse")
}

func TestDbmateComparesVersionsAsText(t *testing.T) {
	m := "-- migrate:up\n" + upA + "-- migrate:down\n"
	base := mk("b", "20250105_a.sql", m)
	head := mk("h", "20250105_a.sql", m, "20250104_b.sql", m)
	r := mustCheck(t, toolDbmate, base, head)
	if codes(r) != "NA101" || !strings.Contains(r.Findings[0].Message, "--strict") {
		t.Fatalf("%q", codes(r))
	}
}

func TestDetectTool(t *testing.T) {
	cases := []struct {
		s    side
		want string
	}{
		{mk("x", "1_a.up.sql", upA), toolMigrate},
		{mk("x", "1_a.sql", "-- +goose Up\n"+upA), toolGoose},
		{mk("x", "1_a.sql", "-- migrate:up\n"+upA), toolDbmate},
		{mk("x", "V1__a.sql", upA), ""},
		{mk("x", "1_a.up.sql", upA, "2_b.sql", "-- +goose Up\n"+upB), ""},
		{mk("x", "notes.txt", "x"), ""},
	}
	for _, c := range cases {
		if got, _ := detectTool(c.s); got != c.want {
			t.Errorf("%v: got %q want %q", c.s.Files[0].Name, got, c.want)
		}
	}
}

// Broken input on either side, with the other side real, must be refused.
func TestBrokenInputIsRefused(t *testing.T) {
	good := func() []string {
		return []string{"1_a.up.sql", "CREATE TABLE a (\n  id int,\n  name text\n);\n", "1_a.down.sql", "DROP TABLE a;\n", "2_b.up.sql", "INSERT INTO a (name) VALUES ('x;y');\n"}
	}
	damage := map[string]string{
		"empty":         "",
		"whitespace":    " \n\t\n",
		"comments only": "-- todo\n/* later */\n",
		"binary":        "\x00\x01\x02CREATE",
		"not utf-8":     "CREATE TABLE \xff\xfe (id int);\n",
		"random text":   "lorem ipsum dolor sit amet\n",
		"half":          "CREATE TABLE a (\n  id int,\n",
		"cut in string": "INSERT INTO a (name) VALUES ('x;y",
		"cut in value":  "INSERT INTO a (name) VALUES ('x;y')",
		"cut comment":   "CREATE TABLE a (id int);\n/* and then",
		"cut dollar":    "CREATE FUNCTION f() RETURNS int AS $$ BEGIN RETURN 1; END;",
		"make failed":   "make: *** [Makefile:12: migrations] Error 2\n",
		"git failed":    "fatal: path 'db/x.sql' does not exist in 'main'\n",
		"go failed":     "# example.com/app\n./main.go:3:1: syntax error: non-declaration statement outside function body\n",
		"panic":         "panic: runtime error: index out of range [3] with length 3\n",
	}
	for name, bad := range damage {
		for _, which := range []string{"base", "head"} {
			b, h := good(), good()
			target := &h
			if which == "base" {
				target = &b
			}
			(*target)[5] = bad
			_, err := check(toolMigrate, mk("b", b...), mk("h", h...), "")
			if _, ok := err.(inputError); !ok {
				t.Errorf("%s on the %s: got %v, want an input error", name, which, err)
			}
		}
	}
	// A broken misnamed file is refused too, not reported as a finding.
	_, err := check(toolMigrate, mk("b", good()...), mk("h", append(good(), "3_c.sql", "CREATE TABLE c (")...), "")
	if _, ok := err.(inputError); !ok {
		t.Errorf("cut misnamed file: %v", err)
	}
	// An empty side.
	mustRefuse(t, toolMigrate, mk("b"), mk("h", good()...), "is empty")
	mustRefuse(t, toolMigrate, mk("b", good()...), mk("h", "README.md", "x\n"), "has no golang-migrate migration files")
}

func TestUnchangedOddFilesAreNotRefused(t *testing.T) {
	// A file both sides have, byte for byte, already ran. Its style isn't
	// this tool's business, even without a final semicolon.
	b := mk("b", "1_a.up.sql", "CREATE TABLE a (id int)", "1_a.down.sql", "")
	h := mk("h", "1_a.up.sql", "CREATE TABLE a (id int)", "1_a.down.sql", "", "2_b.up.sql", upB)
	if r := mustCheck(t, toolMigrate, b, h); len(r.Findings) != 0 {
		t.Fatalf("%q", codes(r))
	}
}

func TestCutOff(t *testing.T) {
	ok := []string{
		"CREATE TABLE a (id int);",
		"CREATE TABLE a (id int); -- done\n",
		"INSERT INTO a VALUES ('it''s');",
		"INSERT INTO a VALUES ('it\\'s');", // MySQL
		"INSERT INTO a VALUES (E'it\\'s');",
		"SELECT $1::int;",
		"CREATE FUNCTION f() RETURNS int AS $body$ SELECT 1; $body$ LANGUAGE sql;",
		"# mysql comment with ' quote\nCREATE TABLE a (id int);",
		"-- +goose StatementBegin\nCREATE FUNCTION f() RETURNS int AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql;\n-- +goose StatementEnd\n",
		"SELECT \"weird;name\" FROM t;",
	}
	for _, s := range ok {
		if why := cutOff([]byte(s), true); why != "" {
			t.Errorf("%q: %s", s, why)
		}
	}
	bad := []string{
		"CREATE TABLE a (id int",
		"CREATE TABLE a (id int)",
		"INSERT INTO a VALUES ('x",
		"/* comment",
		"CREATE FUNCTION f() AS $$ SELECT 1;",
	}
	for _, s := range bad {
		if why := cutOff([]byte(s), true); why == "" {
			t.Errorf("%q: not reported as cut off", s)
		}
	}
}

func TestRunExitCodes(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--base", "a"}, &out, &errb); code != exitUsage {
		t.Errorf("missing head: %d", code)
	}
	if code := run([]string{"x"}, &out, &errb); code != exitUsage {
		t.Errorf("missing base: %d", code)
	}
	if code := run([]string{"--base", "x", "--base-ref", "y", "z"}, &out, &errb); code != exitUsage {
		t.Errorf("both bases: %d", code)
	}
	if code := run([]string{"--base", "x", "--fail-on", "sometimes", "z"}, &out, &errb); code != exitUsage {
		t.Errorf("bad fail-on: %d", code)
	}
	if code := run([]string{"--base", "same", "same"}, &out, &errb); code != exitUsage {
		t.Errorf("same folder: %d", code)
	}
	errb.Reset()
	if code := run([]string{"--base", "does-not-exist", "nor-this"}, &out, &errb); code != exitInput || !strings.Contains(errb.String(), "no result") {
		t.Errorf("missing folders: %d %s", code, errb.String())
	}
}

func TestFailOn(t *testing.T) {
	r := &report{Findings: []finding{{Level: levelWarn}}}
	if !fails(r, levelWarn) || fails(r, levelError) || fails(r, "never") {
		t.Error("warn")
	}
	r = &report{Findings: []finding{{Level: levelNote}}}
	if fails(r, levelWarn) {
		t.Error("note")
	}
}

func TestApplied(t *testing.T) {
	// Version 3 is on main but not released, so no database has run it.
	base := mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB, "3_c.up.sql", upC)
	head := mk("h", "1_a.up.sql", upA, "2_b.up.sql", upB, "3_c.up.sql", "CREATE TABLE c (id bigint);\n", "4_d.up.sql", upA)
	r, err := check(toolMigrate, base, head, "2")
	if err != nil || len(r.Findings) != 0 || r.Changed != 1 {
		t.Fatalf("%v %q changed=%d", err, codes(r), r.Changed)
	}
	// An edit at or below the applied version still counts.
	head = mk("h", "1_a.up.sql", upA, "2_b.up.sql", "CREATE TABLE b (id bigint);\n", "3_c.up.sql", upC)
	if r, _ := check(toolMigrate, base, head, "2"); codes(r) != "NA102" {
		t.Fatalf("%q", codes(r))
	}
	// A new file below the applied version.
	head = mk("h", "1_a.up.sql", upA, "2_b.up.sql", upB, "3_c.up.sql", upC, "02_x.up.sql", upA)
	if r, _ := check(toolMigrate, base, head, "2"); !strings.Contains(codes(r), "NA104") {
		t.Fatalf("%q", codes(r))
	}
	// New below the applied version, but above an unreleased one, is still out of order.
	head = mk("h", "1_a.up.sql", upA, "2_b.up.sql", upB, "3_c.up.sql", upC, "5_e.up.sql", upA)
	if r, _ := check(toolMigrate, base, head, "7"); codes(r) != "NA101" || !strings.Contains(r.Findings[0].Message, "--applied") {
		t.Fatalf("%q", codes(r))
	}
	if _, err := check(toolMigrate, base, head, "v2"); err == nil {
		t.Fatal("bad --applied accepted")
	}
}

func TestSemicolonStyle(t *testing.T) {
	// The project leaves out final semicolons, as harbor does in places,
	// so a complete file without one is accepted.
	b := mk("b", "1_a.up.sql", "CREATE TABLE a (id int)\n", "2_b.up.sql", upB)
	h := mk("h", "1_a.up.sql", "CREATE TABLE a (id int)\n", "2_b.up.sql", upB, "3_c.up.sql", "UPDATE a SET id = 1 WHERE id = 0\n")
	if r := mustCheck(t, toolMigrate, b, h); len(r.Findings) != 0 {
		t.Fatalf("%q", codes(r))
	}
	// Still refused when it ends in the middle of a statement.
	for _, cut := range []string{"UPDATE a SET id = 1 WHERE\n", "UPDATE a SET id =\n", "INSERT INTO a VALUES (1),\n", "SELECT a.\n"} {
		h := mk("h", "1_a.up.sql", "CREATE TABLE a (id int)\n", "2_b.up.sql", upB, "3_c.up.sql", cut)
		mustRefuse(t, toolMigrate, b, h, "cut off")
	}
	// When every unchanged file ends with a semicolon, a missing one is refused.
	b = mk("b", "1_a.up.sql", upA, "2_b.up.sql", upB)
	h = mk("h", "1_a.up.sql", upA, "2_b.up.sql", upB, "3_c.up.sql", "UPDATE a SET id = 1 WHERE id = 0\n")
	mustRefuse(t, toolMigrate, b, h, "doesn't end with a semicolon")
	// goose always needs it, because goose itself fails without it.
	gb := mk("b", "001_a.sql", "-- +goose Up\nCREATE TABLE a (id int)\n")
	gh := mk("h", "001_a.sql", "-- +goose Up\nCREATE TABLE a (id int)\n", "002_b.sql", "-- +goose Up\nCREATE TABLE b (id int)\n")
	mustRefuse(t, toolGoose, gb, gh, "semicolon")
}

func TestGarbled(t *testing.T) {
	bad := []string{
		"psql: error: connection to server on socket \"/tmp/.s.PGSQL.5432\" failed\n",
		"FATAL:  password authentication failed for user \"app\"\n",
		"pg_dump: error: query failed\n",
		"sqlite3: Error: no such table\n",
		"error: pathspec 'x' did not match\n",
	}
	for _, b := range bad {
		if garbled([]byte(b)) == "" {
			t.Errorf("%q not refused", b)
		}
	}
	good := []string{
		"CREATE TABLE errors (id int);\n",
		"-- error: handling table\nCREATE TABLE a (id int);\n",
		"UPDATE jobs SET state = 'failed' WHERE id = 1;\n",
		"INSERT INTO logs (level) VALUES ('error');\n",
	}
	for _, g := range good {
		if why := garbled([]byte(g)); why != "" {
			t.Errorf("%q refused: %s", g, why)
		}
	}
}
