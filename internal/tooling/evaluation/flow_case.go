package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type FlowCase struct {
	ID, Root, InputPath, ExpectedPath, WorkspacePath, SCMPath, Fingerprint string
	Expectation                                                            *FlowExpectation
}

var flowCaseID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func DiscoverFlowCases(suitePath string, suite *FlowSuite, onlyID string) ([]FlowCase, error) {
	if suite == nil {
		return nil, fmt.Errorf("nil suite")
	}
	base := filepath.Dir(suitePath)
	dir := suite.Cases.Directory
	if dir == "" {
		return nil, fmt.Errorf("cases directory is empty")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(base, dir)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	lower := map[string]string{}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if !flowCaseID.MatchString(id) {
			return nil, fmt.Errorf("invalid case id %q", id)
		}
		l := strings.ToLower(id)
		if prev, ok := lower[l]; ok {
			return nil, fmt.Errorf("case IDs collide: %q and %q", prev, id)
		}
		lower[l] = id
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if onlyID != "" {
		i := sort.SearchStrings(ids, onlyID)
		if i >= len(ids) || ids[i] != onlyID {
			return nil, fmt.Errorf("unknown case %q (available: %s)", onlyID, strings.Join(ids, ", "))
		}
		ids = []string{onlyID}
	}
	out := make([]FlowCase, 0, len(ids))
	for _, id := range ids {
		root := filepath.Join(dir, id)
		c := FlowCase{ID: id, Root: root, InputPath: filepath.Join(root, "input.md"), ExpectedPath: filepath.Join(root, "expected.yaml"), WorkspacePath: filepath.Join(root, "workspace")}
		if err := validateRegular(c.InputPath); err != nil {
			return nil, fmt.Errorf("case %s input.md: %w", id, err)
		}
		if err := validateRegular(c.ExpectedPath); err != nil {
			return nil, fmt.Errorf("case %s expected.yaml: %w", id, err)
		}
		if err := validateTree(root); err != nil {
			return nil, fmt.Errorf("case %s: %w", id, err)
		}
		ents, err := os.ReadDir(c.WorkspacePath)
		if err != nil {
			return nil, fmt.Errorf("case %s workspace: %w", id, err)
		}
		if len(ents) == 0 {
			return nil, fmt.Errorf("case %s workspace is empty", id)
		}
		exp, err := LoadFlowExpectation(c.ExpectedPath)
		if err != nil {
			return nil, err
		}
		c.Expectation = exp
		c.Fingerprint, err = FingerprintFlowCase(c)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func validateRegular(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	return nil
}
func validateTree(root string) error {
	return filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink forbidden: %s", rel)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		for _, p := range parts {
			if p == ".git" {
				return fmt.Errorf(".git forbidden: %s", rel)
			}
			if p == ".takt" && len(parts) > 1 {
				switch parts[1] {
				case "eval", "runs", "worktrees", "evals":
					return fmt.Errorf("reserved .takt path: %s", rel)
				}
			}
		}
		return nil
	})
}

func CopyFlowCaseWorkspace(src, dst string) error { return CopyFlowTree(src, dst) }
func CopyFlowTree(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("source is not directory")
	}
	return filepath.WalkDir(src, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := dst
		if rel != "." {
			target = filepath.Join(dst, rel)
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink forbidden: %s", rel)
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, cp := io.Copy(out, in)
		ce := out.Close()
		if cp != nil {
			return cp
		}
		return ce
	})
}

func FingerprintFlowCase(c FlowCase) (string, error) {
	h := sha256.New()
	surfaces := []struct{ name, root string }{{"input.md", c.InputPath}, {"expected.yaml", c.ExpectedPath}, {"workspace", c.WorkspacePath}}
	if c.SCMPath != "" {
		surfaces = append(surfaces, struct{ name, root string }{"scm", c.SCMPath})
	}
	for _, s := range surfaces {
		st, err := os.Lstat(s.root)
		if err != nil {
			return "", err
		}
		if st.IsDir() {
			var files []string
			err = filepath.WalkDir(s.root, func(p string, e os.DirEntry, er error) error {
				if er != nil {
					return er
				}
				if e.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("symlink forbidden: %s", p)
				}
				if !e.IsDir() {
					files = append(files, p)
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			sort.Strings(files)
			for _, p := range files {
				if err := hashOne(h, s.name, s.root, p); err != nil {
					return "", err
				}
			}
		} else if err := hashOne(h, s.name, filepath.Dir(s.root), s.root); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func hashOne(h interface{ Write([]byte) (int, error) }, surface, root, path string) error {
	rel := filepath.Base(path)
	if root != filepath.Dir(path) {
		r, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = r
	}
	name := surface + "/" + filepath.ToSlash(rel)
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(h, "%s\x00%04o\x00", name, st.Mode().Perm())
	h.Write(data)
	h.Write([]byte{0})
	return nil
}
