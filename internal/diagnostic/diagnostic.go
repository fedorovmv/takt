package diagnostic

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"takt/internal/execution"
)

// Value is a stable machine-readable description of an execution failure.
// Message remains human-readable while Fingerprint is computed from normalized
// fields so retries and baseline routing can recognize the same failure.
type Value struct {
	Code        string `json:"code"`
	Kind        string `json:"kind"`
	Op          string `json:"op,omitempty"`
	Message     string `json:"message"`
	Fingerprint string `json:"fingerprint"`
	Retryable   bool   `json:"retryable,omitempty"`
}

var volatileNumber = regexp.MustCompile(`\b(?:pid|process|attempt|port|line)\s*[=:]?\s*\d+\b`)
var longNumber = regexp.MustCompile(`\b\d{5,}\b`)

func FromError(code string, err error, retryable bool, roots ...string) Value {
	kind := string(execution.KindOf(err))
	op := ""
	var execErr *execution.Error
	if errors.As(err, &execErr) {
		op = strings.TrimSpace(execErr.Op)
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	normalized := normalize(message, roots...)
	payload := strings.Join([]string{strings.TrimSpace(code), kind, op, normalized}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return Value{Code: strings.TrimSpace(code), Kind: kind, Op: op, Message: message, Fingerprint: "sha256:" + hex.EncodeToString(sum[:]), Retryable: retryable}
}

func normalize(message string, roots ...string) string {
	clean := strings.TrimSpace(strings.ReplaceAll(message, "\\", "/"))
	canonical := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		abs = filepath.ToSlash(filepath.Clean(abs))
		canonical = append(canonical, abs)
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			canonical = append(canonical, filepath.ToSlash(filepath.Clean(resolved)))
		}
	}
	sort.Slice(canonical, func(i, j int) bool { return len(canonical[i]) > len(canonical[j]) })
	for _, root := range canonical {
		clean = strings.ReplaceAll(clean, root, "<workspace>")
	}
	clean = volatileNumber.ReplaceAllString(clean, "<volatile>")
	clean = longNumber.ReplaceAllString(clean, "<n>")
	clean = strings.Join(strings.Fields(clean), " ")
	return clean
}
