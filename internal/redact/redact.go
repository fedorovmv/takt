package redact

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
)

const SecretPrefix = "secret://"

type Redactor struct {
	mu     sync.RWMutex
	values []string
}

func NewFromEnvironment() *Redactor {
	r := &Redactor{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !sensitiveKey(key) {
			continue
		}
		r.Add(value)
	}
	return r
}

func sensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, marker := range []string{"token", "secret", "password", "passwd", "api_key", "apikey", "private_key", "credential"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}

func (r *Redactor) Add(value string) {
	if len(value) < 6 {
		return
	}
	r.add(value)
}

// AddSecret registers an explicit secret reference without the heuristic
// minimum length used by environment auto-discovery.
func (r *Redactor) AddSecret(value string) {
	r.add(value)
}

func (r *Redactor) add(value string) {
	if r == nil || value == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.values {
		if existing == value {
			return
		}
	}
	r.values = append(r.values, value)
	sort.Slice(r.values, func(i, j int) bool { return len(r.values[i]) > len(r.values[j]) })
}

func (r *Redactor) Resolve(value string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(value), SecretPrefix) {
		return value, nil
	}
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), SecretPrefix))
	if name == "" {
		return "", &MissingSecretError{Name: name}
	}
	resolved, ok := os.LookupEnv(name)
	if !ok || resolved == "" {
		return "", &MissingSecretError{Name: name}
	}
	r.AddSecret(resolved)
	return resolved, nil
}

// RegisterReferences finds explicit secret://ENV_NAME references in a value and
// registers the current environment values for persistence redaction. Missing
// variables are ignored here; Resolve remains the fail-closed execution gate.
func (r *Redactor) RegisterReferences(value string) {
	if r == nil || value == "" {
		return
	}
	for offset := 0; ; {
		index := strings.Index(value[offset:], SecretPrefix)
		if index < 0 {
			return
		}
		start := offset + index + len(SecretPrefix)
		end := start
		for end < len(value) {
			b := value[end]
			if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_') {
				break
			}
			end++
		}
		if end > start {
			if actual, ok := os.LookupEnv(value[start:end]); ok && actual != "" {
				r.AddSecret(actual)
			}
		}
		offset = end
		if offset <= start {
			offset = start + 1
		}
		if offset >= len(value) {
			return
		}
	}
}

type MissingSecretError struct{ Name string }

func (e *MissingSecretError) Error() string {
	if e.Name == "" {
		return "secret reference requires an environment variable name"
	}
	return "secret environment variable " + e.Name + " is not set"
}

func (r *Redactor) String(value string) string {
	if r == nil || value == "" {
		return value
	}
	r.mu.RLock()
	values := append([]string(nil), r.values...)
	r.mu.RUnlock()
	for _, secret := range values {
		value = strings.ReplaceAll(value, secret, "<redacted>")
	}
	return value
}

func (r *Redactor) Bytes(value []byte) ([]byte, bool) {
	text := string(value)
	redacted := r.String(text)
	return []byte(redacted), redacted != text
}

func (r *Redactor) Any(value any) any {
	switch v := value.(type) {
	case string:
		return r.String(v)
	case []byte:
		b, _ := r.Bytes(v)
		return b
	case json.RawMessage:
		if !json.Valid(v) {
			b, _ := r.Bytes(v)
			return json.RawMessage(b)
		}
		var decoded any
		if json.Unmarshal(v, &decoded) == nil {
			b, _ := json.Marshal(r.Any(decoded))
			return json.RawMessage(b)
		}
		return v
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = r.Any(v[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = r.Any(x)
		}
		return out
	default:
		return value
	}
}

func (r *Redactor) Map(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out, _ := r.Any(value).(map[string]any)
	return out
}
