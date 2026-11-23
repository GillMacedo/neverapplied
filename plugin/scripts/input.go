package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// maxFile is the largest migration file read. Bigger files are refused.
const maxFile = 64 << 20

// readDir reads the regular files directly inside dir. Migration tools
// don't look in subfolders, so neither does this.
func readDir(dir string) (side, error) {
	s := side{Label: dir}
	st, err := os.Stat(dir)
	if err != nil {
		return s, untrusted("%s: %s", dir, cleanErr(err))
	}
	if !st.IsDir() {
		return s, untrusted("%s is a file, not a migrations folder. Pass the folder that holds the migration files", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return s, untrusted("%s: %s", dir, cleanErr(err))
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		info, err := os.Stat(p)
		if err != nil {
			return s, untrusted("%s: %s", p, cleanErr(err))
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if info.Size() > maxFile {
			return s, untrusted("%s is larger than %d MB; that isn't a migration file this tool can check", p, maxFile>>20)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return s, untrusted("%s: %s", p, cleanErr(err))
		}
		s.Files = append(s.Files, file{Name: e.Name(), Path: p, Content: b})
	}
	return s, nil
}

// readGit reads the files directly inside the same folder as dir, as they
// are in the git revision ref. It runs only read-only git commands.
func readGit(dir, ref string) (side, error) {
	s := side{}
	if strings.HasPrefix(ref, "-") {
		return s, usageErr("--base-ref %q can't start with '-'", ref)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return s, untrusted("%s isn't a folder", dir)
	}
	prefix, err := git(dir, nil, "rev-parse", "--show-prefix")
	if err != nil {
		return s, untrusted("%s isn't inside a git repository, so --base-ref can't be read: %s", dir, err)
	}
	folder := strings.TrimSuffix(strings.TrimSpace(string(prefix)), "/")
	s.Label = ref + ":" + folder
	if folder == "" {
		s.Label = ref + ":."
	}
	if _, err := git(dir, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return s, untrusted("git has no commit %q here. In CI, fetch the base branch first, for example: git fetch origin main", ref)
	}
	tree := ref + "^{tree}"
	if folder != "" {
		tree = ref + ":" + folder
	}
	out, err := git(dir, nil, "ls-tree", "-z", "--full-tree", tree)
	if err != nil {
		return s, untrusted("%s doesn't exist in %s, so there is no base to compare with: %s", folder, ref, err)
	}
	type entry struct{ name, oid string }
	var entries []entry
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		tab := bytes.IndexByte(rec, '\t')
		if tab < 0 {
			return s, untrusted("unexpected git ls-tree output for %s", tree)
		}
		f := strings.Fields(string(rec[:tab]))
		if len(f) != 3 {
			return s, untrusted("unexpected git ls-tree output for %s", tree)
		}
		// Regular files only, as on disk. 120000 is a symlink, 160000 a submodule.
		if f[1] != "blob" || (f[0] != "100644" && f[0] != "100755") {
			continue
		}
		entries = append(entries, entry{string(rec[tab+1:]), f[2]})
	}
	if len(entries) == 0 {
		return s, nil
	}
	var in bytes.Buffer
	for _, e := range entries {
		in.WriteString(e.oid + "\n")
	}
	out, err = git(dir, &in, "cat-file", "--batch")
	if err != nil {
		return s, untrusted("git cat-file failed: %s", err)
	}
	r := bufio.NewReader(bytes.NewReader(out))
	for _, e := range entries {
		header, err := r.ReadString('\n')
		if err != nil {
			return s, untrusted("git cat-file output ended early")
		}
		f := strings.Fields(header)
		if len(f) != 3 || f[1] != "blob" {
			return s, untrusted("git cat-file: %s", strings.TrimSpace(header))
		}
		n, err := strconv.Atoi(f[2])
		if err != nil || n > maxFile {
			return s, untrusted("git cat-file: bad size for %s", e.name)
		}
		b := make([]byte, n+1)
		if _, err := io.ReadFull(r, b); err != nil {
			return s, untrusted("git cat-file output ended early")
		}
		p := e.name
		if folder != "" {
			p = path.Join(folder, e.name)
		}
		s.Files = append(s.Files, file{Name: e.name, Path: ref + ":" + p, Content: b[:n]})
	}
	sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Name < s.Files[j].Name })
	return s, nil
}

func git(dir string, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdin = stdin
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	// Never prompt, and never let a repository's config page or run tools.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0")
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			msg = err.Error()
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", firstLine(msg))
	}
	return out.Bytes(), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
