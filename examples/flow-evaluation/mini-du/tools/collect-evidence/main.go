package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type evidenceOptions struct {
	Workspace  string
	BaseCommit string
	Output     string
}

type evidenceEntry struct {
	Path string
	Data []byte
	Mode fs.FileMode
	MIME string
}

type manifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
	Mode   string `json:"mode"`
	MIME   string `json:"mime"`
}

func main() {
	options := evidenceOptions{}
	flag.StringVar(&options.Workspace, "workspace", "", "candidate workspace")
	flag.StringVar(&options.BaseCommit, "base", "", "baseline Git commit")
	flag.StringVar(&options.Output, "output", "", "output tar path")
	flag.Parse()
	if err := collect(options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func collect(options evidenceOptions) error {
	workspace, err := filepath.Abs(options.Workspace)
	if err != nil {
		return err
	}
	if options.BaseCommit == "" || options.Output == "" {
		return errors.New("workspace, base, and output are required")
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("candidate workspace is not a directory: %w", err)
	}
	redactor := newEvidenceRedactor()
	if err := scanGitSecrets(workspace, redactor); err != nil {
		return err
	}
	diff, err := workspaceDiff(workspace, options.BaseCommit)
	if err != nil {
		return err
	}
	diff, _ = redactor.Bytes(diff)
	bundle, err := gitBundle(workspace)
	if err != nil {
		return err
	}
	entries := []evidenceEntry{
		{Path: "diff.patch", Data: diff, Mode: 0o644, MIME: "text/x-diff"},
		{Path: "repository.bundle", Data: bundle, Mode: 0o644, MIME: "application/x-git-bundle"},
	}
	source, sourceErr := sourceEntries(workspace, redactor)
	if sourceErr == nil {
		entries = append(entries, source...)
	} else if errors.Is(sourceErr, errSourceUnavailable) {
		entries = append(entries, evidenceEntry{Path: "source-unavailable.txt", Data: []byte(sourceErr.Error() + "\n"), Mode: 0o644, MIME: "text/plain"})
	} else {
		return sourceErr
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	manifest := make([]manifestEntry, len(entries))
	for index, entry := range entries {
		sum := sha256.Sum256(entry.Data)
		manifest[index] = manifestEntry{Path: entry.Path, SHA256: hex.EncodeToString(sum[:]), Size: len(entry.Data), Mode: fmt.Sprintf("%04o", entry.Mode.Perm()), MIME: entry.MIME}
	}
	manifestRaw, err := json.Marshal(struct {
		ProtocolVersion string          `json:"protocol_version"`
		Entries         []manifestEntry `json:"entries"`
	}{ProtocolVersion: "takt-evaluation-evidence/v1alpha1", Entries: manifest})
	if err != nil {
		return err
	}
	entries = append(entries, evidenceEntry{Path: "manifest.json", Data: append(manifestRaw, '\n'), Mode: 0o644, MIME: "application/json"})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return writeArchive(options.Output, entries)
}

var errSourceUnavailable = errors.New("source unavailable")

func sourceEntries(workspace string, redactor *evidenceRedactor) ([]evidenceEntry, error) {
	var entries []evidenceEntry
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		first := strings.Split(filepath.ToSlash(rel), "/")[0]
		if first == ".git" || first == ".takt" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink forbidden: %s", errSourceUnavailable, filepath.ToSlash(rel))
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: non-regular file forbidden: %s", errSourceUnavailable, filepath.ToSlash(rel))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		redacted, matched := redactor.Bytes(data)
		textual := utf8.Valid(data) && !bytes.ContainsRune(data, 0)
		if matched && !textual {
			return fmt.Errorf("binary file contains known secret: %s", filepath.ToSlash(rel))
		}
		if textual {
			data = redacted
		}
		entries = append(entries, evidenceEntry{Path: "source/" + filepath.ToSlash(rel), Data: data, Mode: info.Mode().Perm(), MIME: evidenceMIME(rel, textual)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func workspaceDiff(workspace, base string) ([]byte, error) {
	tracked, err := exec.Command("git", "-C", workspace, "diff", "--binary", base, "--", ".", ":(exclude).takt").Output()
	if err != nil {
		return nil, fmt.Errorf("diff workspace: %w", err)
	}
	untracked, err := exec.Command("git", "-C", workspace, "ls-files", "--others", "--exclude-standard", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("list untracked files: %w", err)
	}
	paths := strings.Split(string(untracked), "\x00")
	sort.Strings(paths)
	result := append([]byte(nil), tracked...)
	for _, path := range paths {
		clean := filepath.Clean(filepath.FromSlash(path))
		if clean == "." || clean == ".takt" || strings.HasPrefix(clean, ".takt"+string(filepath.Separator)) {
			continue
		}
		cmd := exec.Command("git", "diff", "--binary", "--no-index", "--", os.DevNull, clean)
		cmd.Dir = workspace
		patch, diffErr := cmd.Output()
		if diffErr != nil {
			var exit *exec.ExitError
			if !errors.As(diffErr, &exit) || exit.ExitCode() != 1 {
				return nil, fmt.Errorf("diff untracked %s: %w", clean, diffErr)
			}
		}
		result = append(result, patch...)
	}
	return result, nil
}

func gitBundle(workspace string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "takt-evidence-*.bundle")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	defer os.Remove(path)
	if output, err := exec.Command("git", "-C", workspace, "bundle", "create", path, "--all").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("create repository bundle: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return os.ReadFile(path)
}

func scanGitSecrets(workspace string, redactor *evidenceRedactor) error {
	objects, err := exec.Command("git", "-C", workspace, "rev-list", "--objects", "--all").Output()
	if err != nil {
		return fmt.Errorf("list Git history: %w", err)
	}
	for _, line := range strings.Split(string(objects), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		kind, err := exec.Command("git", "-C", workspace, "cat-file", "-t", fields[0]).Output()
		if err != nil || strings.TrimSpace(string(kind)) != "blob" {
			continue
		}
		data, err := exec.Command("git", "-C", workspace, "cat-file", "blob", fields[0]).Output()
		if err != nil {
			return err
		}
		if _, matched := redactor.Bytes(data); matched {
			return fmt.Errorf("git blob contains known secret: %s", fields[0])
		}
	}
	return nil
}

type evidenceRedactor struct{ values []string }

func newEvidenceRedactor() *evidenceRedactor {
	redactor := &evidenceRedactor{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || len(value) < 6 || !evidenceSensitiveKey(key) {
			continue
		}
		redactor.values = append(redactor.values, value)
	}
	sort.Slice(redactor.values, func(i, j int) bool { return len(redactor.values[i]) > len(redactor.values[j]) })
	return redactor
}

func evidenceSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, marker := range []string{"token", "secret", "password", "passwd", "api_key", "apikey", "private_key", "credential"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func (r *evidenceRedactor) Bytes(value []byte) ([]byte, bool) {
	result := append([]byte(nil), value...)
	matched := false
	for _, secret := range r.values {
		if bytes.Contains(result, []byte(secret)) {
			result = bytes.ReplaceAll(result, []byte(secret), []byte("<redacted>"))
			matched = true
		}
	}
	return result, matched
}

func writeArchive(output string, entries []evidenceEntry) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(output), ".evidence-*.tar")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	writer := tar.NewWriter(tmp)
	fixed := time.Unix(0, 0).UTC()
	for _, entry := range entries {
		header := &tar.Header{Name: entry.Path, Mode: int64(entry.Mode.Perm()), Size: int64(len(entry.Data)), Typeflag: tar.TypeReg, ModTime: fixed, AccessTime: fixed, ChangeTime: fixed, Format: tar.FormatPAX}
		if err := writer.WriteHeader(header); err != nil {
			return closeArchive(tmp, writer, err)
		}
		if _, err := writer.Write(entry.Data); err != nil {
			return closeArchive(tmp, writer, err)
		}
	}
	if err := writer.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, output)
}

func closeArchive(file *os.File, writer *tar.Writer, cause error) error {
	_ = writer.Close()
	_ = file.Close()
	return cause
}

func evidenceMIME(path string, textual bool) string {
	if !textual {
		return "application/octet-stream"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".md":
		return "text/markdown"
	default:
		return "text/plain"
	}
}
