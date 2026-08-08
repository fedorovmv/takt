package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"takt/internal/yamlmini"
)

const CaseManifestAPIVersion = "takt/evaluation/v1alpha1"
const CaseManifestKind = "CaseManifest"

type CaseManifest struct {
	APIVersion string                       `json:"apiVersion"`
	Kind       string                       `json:"kind"`
	Cases      map[string]map[string]string `json:"cases"`
}

func loadCaseManifest(path, casesDir string, caseIDs map[string]string) (map[string]map[string]string, string, error) {
	if strings.TrimSpace(path) == "" {
		return map[string]map[string]string{}, "", nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(casesDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", fmt.Errorf("read case manifest: %w", err)
	}
	var manifest CaseManifest
	if err := yamlmini.Unmarshal(data, &manifest); err != nil {
		return nil, "", fmt.Errorf("decode case manifest: %w", err)
	}
	if manifest.APIVersion != CaseManifestAPIVersion || manifest.Kind != CaseManifestKind {
		return nil, "", fmt.Errorf("case manifest must be %s %s", CaseManifestAPIVersion, CaseManifestKind)
	}
	known := map[string]bool{}
	for _, id := range caseIDs {
		known[id] = true
	}
	out := map[string]map[string]string{}
	for id, labels := range manifest.Cases {
		if !known[id] {
			return nil, "", fmt.Errorf("case manifest references unknown case %q", id)
		}
		clean := map[string]string{}
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			k := strings.TrimSpace(key)
			v := strings.TrimSpace(labels[key])
			if k == "" || v == "" {
				return nil, "", fmt.Errorf("case manifest labels for %q must be non-empty", id)
			}
			clean[k] = v
		}
		out[id] = clean
	}
	sum := sha256.Sum256(data)
	return out, hex.EncodeToString(sum[:]), nil
}
