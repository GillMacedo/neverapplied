package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"unicode/utf8"
)

// errorLineRe matches the first line of an error from a named program,
// such as "psql: error: connection to server failed".
var errorLineRe = regexp.MustCompile(`^(?:[A-Za-z0-9_./-]+:\s*){1,2}(?i:error|fatal|panic|failed)\b|^(?:FATAL|PANIC|ERROR)\b`)

// errorPrefixes start the output of a failed command. A migration file
// that starts with one was most likely written by a redirect that failed.
var errorPrefixes = []string{
	"fatal:", "error:", "error ", "Error:", "Error ", "ERROR:", "ERROR ", "panic:", "go: ", "make: ", "make[",
	"bash:", "sh:", "zsh:", "/bin/sh:", "Traceback (most recent call last)", "npm ERR!", "warning: ", "usage:", "Usage:",
	"goose:", "goose run:", "migrate:", "dbmate:", "error: pathspec", "Command failed", "exit status ",
}

// garbled reports why a file can't be a migration at all: binary content,
// text that isn't UTF-8, or the output of a failed command.
func garbled(b []byte) string {
	if bytes.IndexByte(b, 0) >= 0 {
		return "it contains NUL bytes, so it's binary, not a migration"
	}
	if !utf8.Valid(b) {
		return "it isn't valid UTF-8 text"
	}
	line := strings.TrimSpace(firstLine(strings.TrimLeft(strings.TrimPrefix(string(b), "\ufeff"), " \t\r\n")))
	for _, p := range errorPrefixes {
		if strings.HasPrefix(line, p) {
			return "it starts like the output of a failed command: " + quote(line)
		}
	}
	if errorLineRe.MatchString(line) {
		return "it starts like the output of a failed command: " + quote(line)
	}
	return ""
}

// incomplete reports why a migration that changed looks empty or cut off.
// Files that are byte-identical on both sides aren't checked, because
// databases have already run them as they are.
func incomplete(tool string, m migration, needSemicolon bool) string {
	if strings.HasSuffix(m.Name, ".go") {
		if _, err := parser.ParseFile(token.NewFileSet(), m.Name, m.Content, parser.AllErrors); err != nil {
			return "the Go file doesn't parse, so it may be cut off: " + firstLine(err.Error())
		}
		return ""
	}
	if !strings.HasSuffix(m.Name, ".sql") {
		return ""
	}
	up, down := sections(tool, m)
	if tool == toolMigrate && m.Dir == "down" {
		up, down = down, nil
		if isBlank(up) {
			// golang-migrate's create command writes empty down files.
			return ""
		}
	} else if isBlank(up) {
		return "it has no SQL to run on the way up. If it's meant to be empty, add a comment-free statement such as SELECT 1;"
	}
	if why := cutOff(up, needSemicolon); why != "" {
		return why
	}
	if !isBlank(down) {
		if why := cutOff(down, needSemicolon); why != "" {
			return "its down section " + strings.TrimPrefix(why, "it ")
		}
	}
	return ""
}

// isBlank reports whether SQL has nothing but whitespace and comments.
func isBlank(b []byte) bool {
	r := scanSQL(b, false)
	return r.last == 0
}

// dangling are words a complete statement can't end with.
var dangling = map[string]bool{
	"ADD": true, "ALTER": true, "AND": true, "AS": true, "BETWEEN": true, "BY": true, "CASE": true, "CHECK": true,
	"COLUMN": true, "CONSTRAINT": true, "CREATE": true, "DEFAULT": true, "DELETE": true, "DROP": true, "ELSE": true,
	"EXISTS": true, "FROM": true, "IF": true, "IN": true, "INDEX": true, "INSERT": true, "INTO": true, "IS": true,
	"JOIN": true, "LIKE": true, "NOT": true, "ON": true, "OR": true, "REFERENCES": true, "SELECT": true, "SET": true,
	"TABLE": true, "THEN": true, "TO": true, "TYPE": true, "UPDATE": true, "USING": true, "VALUES": true, "WHEN": true,
	"WHERE": true, "WITH": true,
}

// cutOff reports why SQL looks cut off. It accepts the text if it is
// complete when read with either standard SQL quoting or MySQL's
// backslash escapes and # comments. With needSemicolon, the last
// statement must end with a semicolon.
func cutOff(b []byte, needSemicolon bool) string {
	var first string
	for _, mysql := range []bool{false, true} {
		r := scanSQL(b, mysql)
		why := r.why
		if why == "" {
			switch {
			case r.last == ',' || strings.IndexByte("=<>+-*/|.(", r.last) >= 0:
				why = "with " + quote(string(r.last))
			case dangling[strings.ToUpper(r.word)]:
				why = "with " + quote(r.word) + ", in the middle of a statement"
			case needSemicolon && r.last != ';':
				why = "semicolon"
			}
		}
		if why == "" {
			return ""
		}
		if first == "" {
			first = why
		}
	}
	if first == "semicolon" {
		return "its last statement doesn't end with a semicolon, and every unchanged migration here does, so it may be cut off. Add the semicolon if the file is complete"
	}
	return "it ends " + first + ", so it looks cut off"
}

type scanResult struct {
	why  string // why it ends in an open state, or ""
	last byte   // last character outside comments and whitespace
	word string // last word, when the text ends with one
}

// scanSQL walks SQL text and reports whether it ends inside a string,
// comment or parentheses, and how it ends.
func scanSQL(b []byte, mysql bool) scanResult {
	s := string(b)
	n := len(s)
	var r scanResult
	lastPos, depth := -1, 0
	for i := 0; i < n; i++ {
		c := s[i]
		switch {
		case c == '-' && i+1 < n && s[i+1] == '-', mysql && c == '#':
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				i = n
				continue
			}
			i += j
		case c == '/' && i+1 < n && s[i+1] == '*':
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				r.why = "inside a /* comment"
				return r
			}
			i += j + 3
		case c == '\'' || c == '"' || c == '`':
			esc := mysql || (c == '\'' && i > 0 && (s[i-1] == 'E' || s[i-1] == 'e') && (i < 2 || !isIdent(s[i-2])))
			j := closeQuote(s, i+1, c, esc)
			if j < 0 {
				r.why = "inside a quoted string"
				return r
			}
			i = j
			lastPos = i
		case c == '$' && (i == 0 || !isIdent(s[i-1])):
			tag, ok := dollarTag(s, i)
			if !ok {
				lastPos = i
				continue
			}
			j := strings.Index(s[i+len(tag):], tag)
			if j < 0 {
				r.why = "inside a " + tag + " quoted body"
				return r
			}
			i += len(tag) + j + len(tag) - 1
			lastPos = i
		case c == '(':
			depth++
			lastPos = i
		case c == ')':
			depth--
			lastPos = i
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
		default:
			lastPos = i
		}
	}
	if depth > 0 {
		r.why = "with a parenthesis left open"
	}
	if lastPos >= 0 {
		r.last = s[lastPos]
		if isIdent(r.last) && r.last < 0x80 {
			j := lastPos
			for j > 0 && isIdent(s[j-1]) {
				j--
			}
			r.word = s[j : lastPos+1]
		}
	}
	return r
}

func closeQuote(s string, i int, q byte, esc bool) int {
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if esc {
				i++
			}
		case q:
			if i+1 < len(s) && s[i+1] == q {
				i++
				continue
			}
			return i
		}
	}
	return -1
}

// dollarTag reads a PostgreSQL dollar quote opener such as $$ or $body$.
// $1 is a parameter, not a quote.
func dollarTag(s string, i int) (string, bool) {
	j := i + 1
	for j < len(s) && (isIdent(s[j]) && !(j == i+1 && s[j] >= '0' && s[j] <= '9')) {
		j++
	}
	if j < len(s) && s[j] == '$' {
		return s[i : j+1], true
	}
	return "", false
}

func isIdent(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

func quote(s string) string {
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return "\"" + s + "\""
}
