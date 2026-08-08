package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"takt/internal/artifacttype"
	"takt/internal/assistant"
	"takt/internal/redact"
	"takt/internal/spec"
	"takt/internal/store"
)

func (r *Runner) captureDeclaredArtifact(state *store.RunState, node spec.Node, local map[string]store.NodeState) error {
	if strings.TrimSpace(node.OutputType) == "" {
		return nil
	}
	if !artifacttype.Valid(node.OutputType) {
		return fmt.Errorf("output_type must match %s", artifacttype.Pattern)
	}
	ns := state.Nodes[node.ID]
	if ns == nil {
		return fmt.Errorf("node state %q is missing", node.ID)
	}
	mime := strings.TrimSpace(node.OutputMIME)
	if mime == "" {
		if node.OutputFormat != nil {
			mime = "application/json"
		} else {
			mime = "text/plain"
		}
	}
	artifactDir := filepath.Join(r.Store.ArtifactsDir(state.ID), "nodes", safeArtifactPart(node.ID), strconv.Itoa(ns.Attempts))
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	var sourcePath string
	var data []byte
	var filename string
	if strings.TrimSpace(node.OutputPath) != "" {
		rendered, err := renderTemplate(node.OutputPath, state, local, ns.Feedback, r.Store.ArtifactsDir(state.ID))
		if err != nil {
			return fmt.Errorf("render artifact output_path: %w", err)
		}
		resolved, err := r.resolveArtifactSourcePath(rendered, r.Store.ArtifactsDir(state.ID))
		if err != nil {
			return err
		}
		sourcePath = resolved
		info, err := os.Stat(sourcePath)
		if err != nil {
			return fmt.Errorf("artifact output_path %q: %w", rendered, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact output_path %q is not a regular file", rendered)
		}
		filename = filepath.Base(sourcePath)
	} else {
		data = []byte(ns.Output)
		filename = safeArtifactPart(node.OutputType) + artifactExtension(mime)
	}
	destination := filepath.Join(artifactDir, filename)
	if sourcePath != "" {
		var err error
		data, err = os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
	}
	if r.redactor != nil {
		redacted, found := r.redactor.Bytes(data)
		if found {
			if redact.TextualMIME(mime) {
				data = redacted
			} else {
				return fmt.Errorf("artifact %s contains a known secret and cannot be persisted as non-text content", node.OutputType)
			}
		}
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		return err
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	artifact := store.ArtifactRef{
		ID:             fmt.Sprintf("%s:%s:%d", node.ID, node.OutputType, ns.Attempts),
		Type:           node.OutputType,
		MIME:           mime,
		Path:           absolute,
		SHA256:         hex.EncodeToString(sum[:]),
		Size:           int64(len(data)),
		ProducerRunID:  state.ID,
		ProducerNodeID: node.ID,
		Attempt:        ns.Attempts,
		CreatedAt:      time.Now().UTC(),
	}
	ns.Artifacts = appendArtifactUnique(ns.Artifacts, artifact)
	state.Artifacts = appendArtifactUnique(state.Artifacts, artifact)
	return r.commit(state, "assistant."+assistant.EventArtifactDeclared, node.ID, assistant.EventData(assistant.Event{
		Type: assistant.EventArtifactDeclared,
		Artifact: &assistant.ArtifactDeclaration{
			ID: artifact.ID, Type: artifact.Type, MIME: artifact.MIME, Path: artifact.Path,
			SHA256: artifact.SHA256, Size: artifact.Size,
		},
	}))
}

func (r *Runner) resolveArtifactSourcePath(value, artifactsDir string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("artifact output_path is empty")
	}
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(r.Workspace, candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	resolved, err := resolveExistingSymlinkPrefix(filepath.Clean(abs))
	if err != nil {
		return "", err
	}
	allowed := []string{r.Workspace, artifactsDir}
	for _, root := range allowed {
		rootAbs, rootErr := filepath.Abs(root)
		if rootErr != nil {
			continue
		}
		rootAbs, rootErr = resolveExistingSymlinkPrefix(rootAbs)
		if rootErr != nil {
			continue
		}
		rel, relErr := filepath.Rel(rootAbs, resolved)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("artifact output_path %q is outside execution workspace and Run artifacts", value)
}

func resolveExistingSymlinkPrefix(path string) (string, error) {
	path = filepath.Clean(path)
	existing := path
	var suffix []string
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("resolve artifact path %q: no existing parent", path)
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
	evaluated, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		evaluated = filepath.Join(evaluated, suffix[i])
	}
	return filepath.Clean(evaluated), nil
}

func copyArtifactFile(source, destination string) error {
	sourceAbs, _ := filepath.Abs(source)
	destinationAbs, _ := filepath.Abs(destination)
	if filepath.Clean(sourceAbs) == filepath.Clean(destinationAbs) {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = output.Close()
		if !ok {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func appendArtifactUnique(values []store.ArtifactRef, artifact store.ArtifactRef) []store.ArtifactRef {
	for index := range values {
		if values[index].ID == artifact.ID && values[index].ProducerRunID == artifact.ProducerRunID {
			values[index] = artifact
			return values
		}
	}
	return append(values, artifact)
}

func cloneArtifacts(values []store.ArtifactRef) []store.ArtifactRef {
	if len(values) == 0 {
		return nil
	}
	return append([]store.ArtifactRef(nil), values...)
}

func safeArtifactPart(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '_' || char == '-' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "artifact"
	}
	return builder.String()
}

func artifactExtension(mime string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0])) {
	case "application/json":
		return ".json"
	case "text/markdown":
		return ".md"
	case "text/plain":
		return ".txt"
	case "text/html":
		return ".html"
	case "text/csv":
		return ".csv"
	default:
		return ".bin"
	}
}
