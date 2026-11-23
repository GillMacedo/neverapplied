package main

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Tools neverapplied knows. Each has its own file naming and its own way
// of recording which migrations a database has applied.
const (
	toolMigrate = "golang-migrate"
	toolGoose   = "goose"
	toolDbmate  = "dbmate"
)

var tools = []string{toolMigrate, toolGoose, toolDbmate}

// file is one regular file in a migrations folder, from disk or from git.
type file struct {
	Name    string
	Path    string // shown to the user
	Content []byte
}

// side is one of the two inputs: the base (what databases may already
// have applied) or the head (what will be deployed).
type side struct {
	Label string
	Files []file
}

// migration is a file the tool would load, with its parsed version.
type migration struct {
	file
	Version string // as written in the name, digits only
	Num     uint64 // numeric version, for golang-migrate and goose
	Dir     string // "up" or "down" for golang-migrate, "" for single-file tools
	Key     string // what the tool tracks: version, plus direction for golang-migrate
}

// misnamed is a file that looks like a migration but that the tool ignores
// or rejects because of its name.
type misnamed struct {
	file
	Why string
}

var (
	migrateRe    = regexp.MustCompile(`^([0-9]+)_(.*)\.(down|up)\.(.*)$`)
	dbmateRe     = regexp.MustCompile(`^(\d+).*\.sql$`)
	gooseUpRe    = regexp.MustCompile(`(?m)^\s*--\s*\+goose\s+Up\b`)
	gooseDownRe  = regexp.MustCompile(`(?m)^\s*--\s*\+goose\s+Down\b`)
	dbmateUpRe   = regexp.MustCompile(`(?m)^--\s*migrate:up(\s*$|\s+\S+)`)
	dbmateDownRe = regexp.MustCompile(`(?m)^--\s*migrate:down(\s*$|\s+\S+)`)
	flywayRe     = regexp.MustCompile(`^[VUR][0-9_.]*__.*\.sql$`)
	gooseGoRe    = regexp.MustCompile(`goose\.Add(Named)?Migration`)
)

// detectTool guesses the migration tool from both sides' files. It returns
// "" when nothing or more than one tool matches.
func detectTool(sides ...side) (string, []string) {
	found := map[string]bool{}
	flyway := false
	for _, s := range sides {
		for _, f := range s.Files {
			switch {
			case migrateRe.MatchString(f.Name):
				found[toolMigrate] = true
			case strings.HasSuffix(f.Name, ".sql") && gooseUpRe.Match(f.Content):
				found[toolGoose] = true
			case strings.HasSuffix(f.Name, ".go") && gooseGoRe.Match(f.Content):
				found[toolGoose] = true
			case strings.HasSuffix(f.Name, ".sql") && dbmateUpRe.Match(f.Content):
				found[toolDbmate] = true
			case flywayRe.MatchString(f.Name):
				flyway = true
			}
		}
	}
	var names []string
	for _, t := range tools {
		if found[t] {
			names = append(names, t)
		}
	}
	if flyway && len(names) == 0 {
		names = append(names, "flyway")
	}
	if len(names) == 1 && names[0] != "flyway" {
		return names[0], names
	}
	return "", names
}

// load returns the files the tool treats as migrations, and the files it
// ignores or rejects because of their names.
func load(tool string, s side) ([]migration, []misnamed) {
	var ms []migration
	var bad []misnamed
	for _, f := range s.Files {
		switch tool {
		case toolMigrate:
			m := migrateRe.FindStringSubmatch(f.Name)
			if m == nil {
				if strings.HasSuffix(f.Name, ".sql") {
					bad = append(bad, misnamed{f, "the name doesn't match VERSION_TITLE.up.sql or VERSION_TITLE.down.sql, so golang-migrate ignores the file without an error"})
				}
				continue
			}
			n, err := strconv.ParseUint(m[1], 10, 64)
			if err != nil {
				bad = append(bad, misnamed{f, "the version is too large for golang-migrate, which fails to open the folder"})
				continue
			}
			ms = append(ms, migration{file: f, Version: m[1], Num: n, Dir: m[3], Key: strconv.FormatUint(n, 10) + "." + m[3]})
		case toolGoose:
			isGo := strings.HasSuffix(f.Name, ".go") && !strings.HasSuffix(f.Name, "_test.go")
			if !strings.HasSuffix(f.Name, ".sql") && !isGo {
				continue
			}
			i := strings.Index(f.Name, "_")
			if isGo {
				// goose skips Go files without a version prefix, such as helpers.
				if i < 0 {
					continue
				}
				if n, err := strconv.ParseInt(f.Name[:i], 10, 64); err != nil || n < 1 {
					continue
				}
			}
			if i < 0 {
				bad = append(bad, misnamed{f, "the name has no '_' after the version, so goose fails with \"no filename separator '_' found\""})
				continue
			}
			n, err := strconv.ParseInt(f.Name[:i], 10, 64)
			if err != nil || n < 1 {
				bad = append(bad, misnamed{f, "the name doesn't start with a version number of 1 or more, so goose fails to collect migrations"})
				continue
			}
			ms = append(ms, migration{file: f, Version: f.Name[:i], Num: uint64(n), Key: strconv.FormatInt(n, 10)})
		case toolDbmate:
			m := dbmateRe.FindStringSubmatch(f.Name)
			if m == nil {
				if strings.HasSuffix(f.Name, ".sql") {
					bad = append(bad, misnamed{f, "the name doesn't start with a version number, so dbmate ignores the file without an error"})
				}
				continue
			}
			ms = append(ms, migration{file: f, Version: m[1], Key: m[1]})
		}
	}
	sort.SliceStable(ms, func(i, j int) bool { return less(tool, ms[i], ms[j]) })
	return ms, bad
}

// less orders migrations the way the tool applies them.
func less(tool string, a, b migration) bool {
	if tool == toolDbmate {
		// dbmate sorts by file name and compares versions as text.
		return a.Name < b.Name
	}
	if a.Num != b.Num {
		return a.Num < b.Num
	}
	return a.Name < b.Name
}

// below reports whether version a sorts before b for the tool.
func below(tool string, a, b migration) bool {
	if tool == toolDbmate {
		return a.Version < b.Version
	}
	return a.Num < b.Num
}

// sections splits a migration into the part that runs on the way up and
// the part that runs on rollback. golang-migrate keeps them in two files.
func sections(tool string, m migration) (up, down []byte) {
	c := m.Content
	switch tool {
	case toolMigrate:
		if m.Dir == "down" {
			return nil, c
		}
		return c, nil
	case toolGoose:
		if strings.HasSuffix(m.Name, ".go") {
			return c, nil
		}
		u := gooseUpRe.FindIndex(c)
		d := gooseDownRe.FindIndex(c)
		return split(c, u, d)
	case toolDbmate:
		u := dbmateUpRe.FindIndex(c)
		d := dbmateDownRe.FindIndex(c)
		return split(c, u, d)
	}
	return c, nil
}

func split(c []byte, u, d []int) (up, down []byte) {
	switch {
	case u == nil && d == nil:
		return c, nil
	case u != nil && d == nil:
		return c[u[1]:], nil
	case u == nil && d != nil:
		return c[:d[0]], c[d[1]:]
	case u[0] < d[0]:
		return c[u[1]:d[0]], c[d[1]:]
	default:
		return c[u[1]:], c[d[1]:u[0]]
	}
}

// squash removes all whitespace, to tell a formatting change from a real one.
func squash(b []byte) []byte {
	return bytes.Join(bytes.Fields(b), nil)
}
