package main

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Finding levels, worst first.
const (
	levelError = "error"
	levelWarn  = "warn"
	levelNote  = "note"
)

type finding struct {
	Code    string `json:"code"`
	Level   string `json:"level"`
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

type report struct {
	Tool        string    `json:"tool"`
	Base        string    `json:"base"`
	Head        string    `json:"head"`
	BaseCount   int       `json:"baseMigrations"`
	HeadCount   int       `json:"headMigrations"`
	BaseHighest string    `json:"baseHighestVersion"`
	Applied     string    `json:"applied,omitempty"`
	New         int       `json:"new"`
	Changed     int       `json:"changed"`
	Removed     int       `json:"removed"`
	Findings    []finding `json:"findings"`
}

// check compares the base and head migrations for the tool. It returns an
// inputError when either side can't be trusted.
func check(tool string, base, head side, applied string) (*report, error) {
	bm, bbad := load(tool, base)
	hm, hbad := load(tool, head)
	for _, x := range []struct {
		s    side
		ms   []migration
		bad  []misnamed
		name string
	}{{base, bm, bbad, "base"}, {head, hm, hbad, "head"}} {
		if len(x.ms) == 0 {
			msg := fmt.Sprintf("the %s, %s, has no %s migration files", x.name, x.s.Label, tool)
			if len(x.s.Files) == 0 {
				msg = fmt.Sprintf("the %s, %s, is empty", x.name, x.s.Label)
			} else if len(x.bad) > 0 {
				msg += fmt.Sprintf(" (%d .sql files with names %s doesn't load)", len(x.bad), tool)
			}
			return nil, untrusted("%s. There is nothing to compare, so there is no result", msg)
		}
		for _, m := range x.ms {
			if why := garbled(m.Content); why != "" {
				return nil, untrusted("%s can't be trusted: %s", m.Path, why)
			}
		}
		for _, m := range x.bad {
			if why := garbled(m.Content); why != "" {
				return nil, untrusted("%s can't be trusted: %s", m.Path, why)
			}
		}
	}
	// Files that are byte-identical on both sides have already run as they
	// are. Every other file must look complete.
	settled := func(m migration, other []file) bool {
		for _, f := range other {
			if f.Name == m.Name && bytes.Equal(f.Content, m.Content) {
				return true
			}
		}
		return false
	}
	needSemicolon := tool == toolGoose || allEndWithSemicolon(tool, bm, head.Files, settled)
	for _, x := range []struct {
		ms    []migration
		other []file
	}{{bm, head.Files}, {hm, base.Files}} {
		for _, m := range x.ms {
			if settled(m, x.other) {
				continue
			}
			if why := incomplete(tool, m, needSemicolon); why != "" {
				return nil, untrusted("%s can't be trusted: %s", m.Path, why)
			}
		}
	}
	for _, x := range []struct {
		bad   []misnamed
		other []file
	}{{bbad, head.Files}, {hbad, base.Files}} {
		for _, b := range x.bad {
			m := migration{file: b.file}
			if settled(m, x.other) {
				continue
			}
			if why := incomplete(tool, m, needSemicolon); why != "" {
				return nil, untrusted("%s can't be trusted: %s", m.Path, why)
			}
		}
	}

	r := &report{Tool: tool, Base: base.Label, Head: head.Label, BaseCount: len(bm), HeadCount: len(hm), Findings: []finding{}}
	add := func(f finding) {
		if !strings.HasSuffix(f.Message, ".") {
			f.Message += "."
		}
		r.Findings = append(r.Findings, f)
	}

	byKey := func(ms []migration) map[string][]migration {
		m := map[string][]migration{}
		for _, x := range ms {
			m[x.Key] = append(m[x.Key], x)
		}
		return m
	}
	bk, hk := byKey(bm), byKey(hm)

	// The highest version on the base is where an up-to-date database is.
	var top migration
	for _, m := range bm {
		if m.Dir == "down" {
			continue
		}
		if top.Name == "" || below(tool, top, m) {
			top = m
		}
	}
	if top.Name == "" {
		top = bm[len(bm)-1]
	}
	r.BaseHighest = top.Version
	baseTop := top
	where := "the highest version on the base"
	if applied != "" {
		n, err := strconv.ParseUint(applied, 10, 64)
		if err != nil {
			return nil, usageErr("--applied must be a version number such as 20251103120000, not %q", applied)
		}
		top = migration{file: file{Name: applied}, Version: applied, Num: n, Key: applied}
		where = "the highest version applied to a database (--applied)"
		r.Applied = applied
	}
	// ran reports whether a database may already have applied m.
	ran := func(m migration) bool { return !below(tool, top, m) }

	// Highest version anywhere, to suggest free versions.
	highest := baseTop
	for _, m := range hm {
		if below(tool, highest, m) {
			highest = m
		}
	}
	next := nextVersions(tool, highest)

	baseNames := map[string]bool{}
	for _, m := range bm {
		baseNames[m.Name] = true
	}

	// Duplicate versions in the head.
	dups := map[string]bool{}
	for _, k := range sortedKeys(hk) {
		ms := hk[k]
		if len(ms) < 2 {
			continue
		}
		dups[k] = true
		var names []string
		at := ms[len(ms)-1]
		for _, m := range ms {
			names = append(names, m.Name)
			if !baseNames[m.Name] {
				at = m
			}
		}
		if !baseNames[at.Name] && bk[k] != nil {
			r.New++
		}
		add(finding{Code: "NA104", Level: levelError, Path: at.Path, Version: at.Version,
			Message: fmt.Sprintf("version %s is used by more than one file: %s. %s", ms[0].Version, strings.Join(names, ", "), duplicateEffect(tool, ms[0].Version)),
			Fix:     dupFix(tool, at, baseNames[at.Name], top, next)})
	}

	// Migrations on both sides that changed, and migrations that are gone.
	removed := map[string]migration{}
	for _, k := range sortedKeys(bk) {
		b := bk[k]
		h, ok := hk[k]
		if !ok {
			for _, m := range b {
				removed[m.Name] = m
			}
			continue
		}
		if len(b) != 1 || len(h) != 1 {
			continue
		}
		if !ran(b[0]) {
			// No database has run it yet, so editing it is fine.
			if !bytes.Equal(b[0].Content, h[0].Content) {
				r.Changed++
			}
			continue
		}
		if f, changed := compare(tool, b[0], h[0], top); f != nil {
			if changed {
				r.Changed++
			}
			add(*f)
		} else if changed {
			r.Changed++
		}
	}

	// New migrations: below the base's highest version, or a renamed one.
	renamedFrom := map[string]bool{}
	for _, m := range hm {
		k := m.Key
		if _, ok := bk[k]; ok {
			continue
		}
		{
			r.New++
			up, _ := sections(tool, m)
			for _, name := range sortedNames(removed) {
				old := removed[name]
				oup, _ := sections(tool, old)
				if ran(old) && old.Dir == m.Dir && len(squash(up)) > 0 && bytes.Equal(squash(oup), squash(up)) {
					renamedFrom[name] = true
					add(finding{Code: "NA103", Level: levelError, Path: m.Path, Version: m.Version,
						Message: fmt.Sprintf("this is %s from the base under a new version, %s instead of %s. %s records versions, so a database that already ran %s treats this as a new migration and runs the same SQL again", old.Name, m.Version, old.Version, tool, old.Version),
						Fix:     "restore " + old.Name + " under its old name, and put any change in a new migration"})
					break
				}
			}
			if m.Dir == "down" {
				continue
			}
			if dups[k] {
				continue
			}
			if below(tool, m, top) {
				fix := "rename it to " + rename(m, next(m.Version)) + " so it sorts after " + top.Version
				if tool == toolMigrate {
					fix += ", and its .down file to the same version"
				}
				add(finding{Code: "NA101", Level: levelError, Path: m.Path, Version: m.Version,
					Message: fmt.Sprintf("version %s is below %s, %s. %s", m.Version, top.Version, where, outOfOrderEffect(tool, top.Version)),
					Fix:     fix})
			}
		}
	}
	for _, m := range hm {
		if !settled(m, base.Files) {
			if f := markers(tool, m); f != nil {
				add(*f)
			}
		}
	}

	for _, name := range sortedNames(removed) {
		m := removed[name]
		r.Removed++
		if renamedFrom[name] || !ran(m) {
			continue
		}
		f := finding{Code: "NA203", Level: levelWarn, Path: m.Path, Version: m.Version,
			Fix: "restore the file unless every database, including new ones, is meant to skip it"}
		switch {
		case tool == toolMigrate && m.Dir == "down":
			f.Message = fmt.Sprintf("the down file for version %s was removed, so golang-migrate can't roll back past %s", m.Version, m.Version)
		case tool == toolMigrate && m.Num == top.Num:
			f.Level = levelError
			f.Code = "NA103"
			f.Message = fmt.Sprintf("version %s was removed, and it is %s. golang-migrate up fails with \"no migration found for version %s\" on a database already at %s", m.Version, where, m.Version, m.Version)
		default:
			f.Message = fmt.Sprintf("version %s was removed. Databases that ran it keep its changes, and new databases won't have them, so the two can differ", m.Version)
		}
		add(f)
	}

	// Files the tool ignores or rejects because of their names.
	for _, x := range hbad {
		f := finding{Code: "NA105", Level: levelError, Path: x.Path, Message: x.Why,
			Fix: "rename it to the tool's pattern, with a version above " + top.Version}
		for _, b := range bbad {
			if b.Name == x.Name && bytes.Equal(b.Content, x.Content) {
				f.Level = levelWarn
				f.Message += ". It is the same on the base, so no database has run it either"
			}
		}
		add(f)
	}

	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if rank(a.Level) != rank(b.Level) {
			return rank(a.Level) < rank(b.Level)
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Path < b.Path
	})
	return r, nil
}

// compare reports a migration present on both sides that changed.
func compare(tool string, b, h migration, top migration) (*finding, bool) {
	if bytes.Equal(b.Content, h.Content) {
		if b.Name != h.Name {
			return &finding{Code: "NA301", Level: levelNote, Path: h.Path, Version: h.Version,
				Message: fmt.Sprintf("renamed from %s with the same version and content. %s tracks versions, so databases aren't affected", b.Name, tool)}, false
		}
		return nil, false
	}
	bu, bd := sections(tool, b)
	hu, hd := sections(tool, h)
	switch {
	case !bytes.Equal(squash(bu), squash(hu)):
		f := &finding{Code: "NA102", Level: levelError, Path: h.Path, Version: h.Version,
			Message: fmt.Sprintf("version %s is on the base and its SQL changed. %s records only that a version ran, so a database that already applied %s never runs the new SQL, and only new databases get it", h.Version, tool, h.Version),
			Fix:     fmt.Sprintf("restore %s as it is on the base, and put the change in a new migration above %s", b.Name, top.Version)}
		if strings.HasSuffix(h.Name, ".go") {
			f.Message = fmt.Sprintf("version %s is on the base and its Go code changed. goose records only that a version ran, so a database that already applied %s never runs the new code", h.Version, h.Version)
		}
		return f, true
	case !bytes.Equal(squash(bd), squash(hd)):
		return &finding{Code: "NA202", Level: levelWarn, Path: h.Path, Version: h.Version,
			Message: fmt.Sprintf("the rollback SQL of version %s changed. Databases are unaffected until it's rolled back, but check that it still undoes what the up SQL did", h.Version)}, true
	default:
		return &finding{Code: "NA301", Level: levelNote, Path: h.Path, Version: h.Version,
			Message: "only whitespace changed, which doesn't affect databases"}, true
	}
}

// markers reports a new SQL migration missing the comments the tool needs.
func markers(tool string, m migration) *finding {
	if !strings.HasSuffix(m.Name, ".sql") {
		return nil
	}
	switch tool {
	case toolGoose:
		if !gooseUpRe.Match(m.Content) {
			return &finding{Code: "NA106", Level: levelError, Path: m.Path, Version: m.Version,
				Message: "the file has no \"-- +goose Up\" line, so goose fails to parse it and stops",
				Fix:     "start the file with -- +goose Up"}
		}
	case toolDbmate:
		var miss []string
		if !dbmateUpRe.Match(m.Content) {
			miss = append(miss, "\"-- migrate:up\"")
		}
		if !dbmateDownRe.Match(m.Content) {
			miss = append(miss, "\"-- migrate:down\"")
		}
		if len(miss) > 0 {
			return &finding{Code: "NA106", Level: levelError, Path: m.Path, Version: m.Version,
				Message: "the file has no " + strings.Join(miss, " or ") + " line, and dbmate stops on a migration without both",
				Fix:     "add the missing line; the down section may be empty"}
		}
	}
	return nil
}

func outOfOrderEffect(tool, top string) string {
	switch tool {
	case toolMigrate:
		return fmt.Sprintf("golang-migrate stores only the latest version a database has applied, so a database already at %s never runs this file, and reports no error", top)
	case toolGoose:
		return fmt.Sprintf("goose up stops with \"found N missing migrations before current version %s\" on a database already at %s. With -allow-missing it runs it after newer migrations instead", top, top)
	case toolDbmate:
		return fmt.Sprintf("dbmate runs it after newer migrations on a database already at %s, in a different order from a new database, and dbmate --strict refuses it", top)
	}
	return ""
}

func duplicateEffect(tool, v string) string {
	switch tool {
	case toolMigrate:
		return "golang-migrate fails to open the folder with \"duplicate migration file\""
	case toolGoose:
		return fmt.Sprintf("goose stops with \"duplicate version %s detected\"", strings.TrimLeft(v, "0"))
	case toolDbmate:
		return fmt.Sprintf("On a database that already applied %s, dbmate skips the other file without an error. On a new database it fails with a unique constraint error on schema_migrations", v)
	}
	return ""
}

// nextVersions returns a function that hands out free versions above the
// highest one, keeping the width of the existing versions.
func nextVersions(tool string, highest migration) func(string) string {
	n := highest.Num
	if tool == toolDbmate {
		v, err := strconv.ParseUint(highest.Version, 10, 64)
		if err != nil {
			return func(string) string { return "a version above " + highest.Version }
		}
		n = v
	}
	width := len(highest.Version)
	given := map[string]string{}
	return func(old string) string {
		if s, ok := given[old]; ok {
			return s
		}
		n++
		s := strconv.FormatUint(n, 10)
		for len(s) < width {
			s = "0" + s
		}
		given[old] = s
		return s
	}
}

func rename(m migration, v string) string {
	if strings.HasPrefix(v, "a version") {
		return v
	}
	return v + strings.TrimPrefix(m.Name, m.Version)
}

func rank(l string) int {
	switch l {
	case levelError:
		return 0
	case levelWarn:
		return 1
	}
	return 2
}

func sortedKeys(m map[string][]migration) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortedNames(m map[string]migration) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func dupFix(tool string, at migration, onBase bool, top migration, next func(string) string) string {
	if onBase {
		return "give each file its own version; keep the version of the file that databases already ran"
	}
	fix := "rename " + at.Name + " to " + rename(at, next(at.Version)) + ", above " + top.Version
	if tool == toolMigrate {
		fix += ", and its other direction file to the same version"
	}
	return fix
}

// allEndWithSemicolon reports whether every SQL migration that is the same
// on both sides ends its statements with a semicolon. golang-migrate and
// dbmate send a file to the database whole, so a final semicolon is
// optional there, and some projects leave it out. When this project
// always writes it, a changed file without one is likely cut off.
func allEndWithSemicolon(tool string, ms []migration, other []file, settled func(migration, []file) bool) bool {
	for _, m := range ms {
		if !strings.HasSuffix(m.Name, ".sql") || !settled(m, other) {
			continue
		}
		up, down := sections(tool, m)
		for _, part := range [][]byte{up, down} {
			if r := scanSQL(part, false); r.last != 0 && r.last != ';' {
				return false
			}
		}
	}
	return true
}
