package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"takt/internal/spec"
)

func TestArchonTargetRootLoads(t *testing.T) {
	path := writeArchonWorkflow(t, `name: target-root
description: target contract
provider: pi
model: large
nodes:
  - id: build-node
    bash: printf ok
`)
	if _, err := Load(path); err != nil {
		t.Fatalf("Load(target root): %v", err)
	}
}

func TestArchonTargetRootRequiresPublicFields(t *testing.T) {
	fields := map[string]string{
		"name":  "name: required\n",
		"nodes": "nodes:\n  - id: run\n    bash: true\n",
	}
	for missing := range fields {
		t.Run(missing, func(t *testing.T) {
			source := ""
			for name, value := range fields {
				if name != missing {
					source += value
				}
			}
			if _, err := Load(writeArchonWorkflow(t, source)); err == nil {
				t.Fatalf("target root without %q was accepted", missing)
			}
		})
	}
}

func TestArchonLegacyRootIsRejected(t *testing.T) {
	cases := map[string]string{
		"apiVersion": "apiVersion: takt/v1alpha1\nname: legacy\nnodes:\n  - id: run\n    bash: true\n",
		"kind":       "kind: Workflow\nname: legacy\nnodes:\n  - id: run\n    bash: true\n",
		"metadata":   "metadata:\n  name: legacy\nnodes:\n  - id: run\n    bash: true\n",
		"defaults":   "name: legacy\ndefaults:\n  model: large\nnodes:\n  - id: run\n    bash: true\n",
	}
	for field, source := range cases {
		t.Run(field, func(t *testing.T) {
			if _, err := Load(writeArchonWorkflow(t, source)); err == nil {
				t.Fatalf("legacy root field %q was accepted", field)
			}
		})
	}
}

func TestArchonOutputFormatRemainsUnchanged(t *testing.T) {
	path := writeArchonWorkflow(t, `name: structured
provider: pi
model: large
nodes:
  - id: assess
    prompt: assess
    output_format:
      type: object
      properties:
        result:
          type: object
          properties:
            status:
              type: string
          required: [status]
          additionalProperties: false
      required: [result]
      additionalProperties: false
`)
	wf, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	format := wf.Nodes[0].OutputFormat
	if format == nil || format.Properties["result"].Properties["status"].Type != "string" {
		t.Fatalf("output_format changed during load: %#v", format)
	}
}

func TestArchonPinnedA0FixtureLoadsAndPreservesNodes(t *testing.T) {
	wf, err := Load(filepath.Join("testdata", "archon", "archon-feature-development.yaml"))
	if err != nil {
		t.Fatalf("Load(pinned A0 fixture): %v", err)
	}
	want := []string{"implement", "create-pr", "verify-pr-base"}
	if len(wf.Nodes) != len(want) {
		t.Fatalf("pinned fixture nodes = %d, want %d", len(wf.Nodes), len(want))
	}
	for i, id := range want {
		if wf.Nodes[i].ID != id {
			t.Fatalf("pinned fixture node[%d] = %q, want %q", i, wf.Nodes[i].ID, id)
		}
	}
}

func TestArchonPinnedA1FixtureNormalizesLoop(t *testing.T) {
	wf, err := Load(filepath.Join("testdata", "archon", "t1-fix-issue.yaml"))
	if err != nil {
		t.Fatalf("Load(pinned A1 fixture): %v", err)
	}
	var build *spec.Node
	for index := range wf.Nodes {
		if wf.Nodes[index].ID == "build" {
			build = &wf.Nodes[index]
			break
		}
	}
	if build == nil || build.LoopGroup == nil || build.LoopGroup.Until.Signal != "BUILD-CLEAN" {
		t.Fatalf("pinned A1 loop was not normalized: %#v", build)
	}
}

func TestArchonMalformedA1FieldsAreRejected(t *testing.T) {
	cases := map[string]string{
		"signal": `    loop_group:
      until:
        node: check
        signal: not-valid
      max_iterations: 2
      nodes:
        - id: check
          bash: true
`,
		"requires": `    loop_group:
      until:
        node: review
        requires:
          - node: missing
            exit_code: 0
      max_iterations: 2
      nodes:
        - id: check
          bash: true
        - id: review
          depends_on: [check]
          bash: true
`,
	}
	for name, action := range cases {
		t.Run(name, func(t *testing.T) {
			source := "name: a1-gated\nprovider: pi\nmodel: large\nnodes:\n  - id: gated\n" + action
			if _, err := Load(writeArchonWorkflow(t, source)); err == nil {
				t.Fatalf("A1 field %q was accepted during A0", name)
			}
		})
	}
}

func writeArchonWorkflow(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workflow.yaml")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArchonPinnedFixturesHaveRecordedDigests(t *testing.T) {
	type source struct{ sha, path, url string }
	pinned := map[string]source{
		"archon-feature-development.yaml": {
			sha:  "438ec22dff9cddeee9dfdf2686abc46ab80dfb147081654e7990b24dfadc1895",
			path: ".archon/workflows/defaults/archon-feature-development.yaml",
			url:  "https://raw.githubusercontent.com/coleam00/Archon/41765d6a1448da73f398a30e161f3b4eaba0b768/.archon/workflows/defaults/archon-feature-development.yaml",
		},
		"t1-fix-issue.yaml": {
			sha:  "592a223ca26b66eb260e6589ca768becb111ca83be0be89733c5cda788f831ad",
			path: ".archon/workflows/rasmus-tests/t1-fix-issue.yaml",
			url:  "https://raw.githubusercontent.com/coleam00/Archon/41765d6a1448da73f398a30e161f3b4eaba0b768/.archon/workflows/rasmus-tests/t1-fix-issue.yaml",
		},
	}
	base := filepath.Join("testdata", "archon")
	raw, err := os.ReadFile(filepath.Join(base, "provenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provenance struct {
		Repository  string `json:"repository"`
		Commit      string `json:"commit"`
		License     string `json:"license"`
		LicenseFile string `json:"license_file"`
		LicenseSHA  string `json:"license_sha256"`
		Fixtures    []struct {
			File         string `json:"file"`
			OriginalPath string `json:"original_path"`
			URL          string `json:"url"`
			SHA256       string `json:"sha256"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &provenance); err != nil {
		t.Fatalf("decode provenance: %v", err)
	}
	if provenance.Repository != "https://github.com/coleam00/Archon" || provenance.Commit != "41765d6a1448da73f398a30e161f3b4eaba0b768" || provenance.License != "MIT" || provenance.LicenseFile != "LICENSE" || provenance.LicenseSHA != "be39e59fb1d5f09c831ab6d8d38cba6d07812afb27eed300520dc493580cc7f6" {
		t.Fatalf("unexpected provenance metadata: %#v", provenance)
	}
	license, err := os.ReadFile(filepath.Join(base, provenance.LicenseFile))
	if err != nil {
		t.Fatalf("license fixture: %v", err)
	}
	licenseDigest := sha256.Sum256(license)
	if got := hex.EncodeToString(licenseDigest[:]); got != provenance.LicenseSHA {
		t.Fatalf("license sha256 = %s, want %s", got, provenance.LicenseSHA)
	}
	if len(provenance.Fixtures) != len(pinned) {
		t.Fatalf("provenance fixtures = %d, want %d", len(provenance.Fixtures), len(pinned))
	}
	for _, fixture := range provenance.Fixtures {
		want, ok := pinned[fixture.File]
		if !ok || fixture.SHA256 != want.sha || fixture.OriginalPath != want.path || fixture.URL != want.url {
			t.Fatalf("unexpected fixture provenance: %#v", fixture)
		}
		contents, err := os.ReadFile(filepath.Join(base, fixture.File))
		if err != nil {
			t.Fatal(err)
		}
		actual := sha256.Sum256(contents)
		if got := hex.EncodeToString(actual[:]); got != want.sha {
			t.Fatalf("%s sha256 = %s, want %s", fixture.File, got, want.sha)
		}
	}
}
