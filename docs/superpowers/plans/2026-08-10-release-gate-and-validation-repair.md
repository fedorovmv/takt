# Release Gate and Validation Repair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore the release gate and enforce the existing single-document and bounded-`when` authoring contracts.

**Architecture:** Keep `internal/yamlcodec` as the only YAML/JSON decode boundary and reuse the standard second-`Decode`/`io.EOF` pattern already used by Takt protocols. Keep `internal/whenexpr` deliberately small by validating quote delimiters in `literal`, and enforce the existing TypeScript smoke only in CI with pinned TypeScript 5.7.2.

**Tech Stack:** Go 1.23, `encoding/json`, `io`, `go.yaml.in/yaml/v3`, GitHub Actions, Node 22, TypeScript 5.7.2.

## Global Constraints

- Do not change the public `takt/v1alpha1` workflow language.
- Do not add dependencies beyond the module graph required by the existing pinned Go modules.
- Write and run regression tests red before modifying production behavior.
- Keep local TypeScript smoke optional; require it in GitHub Actions.
- Do not modify historical release documents or the filesystem Store design.

---

### Task 1: Normalize the Go module graph

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: the existing Go 1.23 module declaration and pinned direct dependencies.
- Produces: a module graph that `go vet` and `go test` can load without requesting edits.

- [ ] **Step 1: Confirm the clean-checkout failure**

Run: `go vet ./...`

Expected: FAIL with the missing `gopkg.in/check.v1` checksum diagnostic.

- [ ] **Step 2: Let the Go toolchain normalize the graph**

Run: `go mod tidy`

Expected diff:

```go
require golang.org/x/text v0.14.0 // indirect
```

and `gopkg.in/check.v1` module/content checksums in `go.sum`.

- [ ] **Step 3: Verify module loading**

Run: `go vet ./...`

Expected: PASS.

- [ ] **Step 4: Commit the module repair**

```bash
git add go.mod go.sum
git commit -m "fix: restore Go module graph"
```

---

### Task 2: Reject trailing JSON and YAML documents

**Files:**
- Modify: `internal/yamlcodec/yamlcodec_test.go`
- Modify: `internal/yamlcodec/yamlcodec.go`

**Interfaces:**
- Consumes: `yamlcodec.Unmarshal(data []byte, out any) error`.
- Produces: the same function signature with exactly-one-document enforcement.

- [ ] **Step 1: Add failing regressions and align folded YAML semantics**

Add to `internal/yamlcodec/yamlcodec_test.go`:

```go
func TestRejectsAdditionalDocuments(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{name: "json", src: `{"known":"first"} {"known":"second"}`},
		{name: "yaml", src: "known: first\n---\nknown: second\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got struct {
				Known string `json:"known"`
			}
			if err := Unmarshal([]byte(tc.src), &got); err == nil {
				t.Fatal("expected additional-document error")
			}
		})
	}
}

func TestRejectsInvalidTrailingJSON(t *testing.T) {
	var got struct {
		Known string `json:"known"`
	}
	if err := Unmarshal([]byte(`{"known":"first"} trailing`), &got); err == nil {
		t.Fatal("expected trailing JSON error")
	}
}
```

Change the existing folded expectation to:

```go
wantFolded := "first second\nthird\n"
```

- [ ] **Step 2: Run the focused tests red**

Run: `go test ./internal/yamlcodec -run 'TestRejects(AdditionalDocuments|InvalidTrailingJSON)' -count=1`

Expected: FAIL because both decoders currently ignore trailing input after the first value.

- [ ] **Step 3: Enforce one decoded value**

Add `io` to `internal/yamlcodec/yamlcodec.go`. After the first JSON decode, add:

```go
var extra any
if err := dec.Decode(&extra); err != io.EOF {
	if err == nil {
		return fmt.Errorf("decode JSON document: multiple documents")
	}
	return fmt.Errorf("decode JSON document trailing data: %w", err)
}
```

Replace `yaml.Unmarshal` with a `yaml.NewDecoder`, then perform the same check:

```go
dec := yaml.NewDecoder(strings.NewReader(string(data)))
if err := dec.Decode(&value); err != nil {
	return fmt.Errorf("decode YAML document: %w", err)
}
var extra any
if err := dec.Decode(&extra); err != io.EOF {
	if err == nil {
		return fmt.Errorf("decode YAML document: multiple documents")
	}
	return fmt.Errorf("decode YAML document trailing data: %w", err)
}
```

- [ ] **Step 4: Run the package tests green**

Run: `go test ./internal/yamlcodec -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the strict document boundary**

```bash
git add internal/yamlcodec/yamlcodec.go internal/yamlcodec/yamlcodec_test.go
git commit -m "fix: reject trailing workflow documents"
```

---

### Task 3: Reject malformed quoted `when` literals

**Files:**
- Modify: `internal/whenexpr/whenexpr_test.go`
- Modify: `internal/whenexpr/whenexpr.go`

**Interfaces:**
- Consumes: `whenexpr.Validate(expr string) error` and the existing literal syntax.
- Produces: early validation errors for unmatched quote delimiters without new operators or escape rules.

- [ ] **Step 1: Add malformed literals to the invalid contract table**

Append to the existing `invalid` slice in `TestConstitutionAllowsOnlySmallGateLanguage`:

```go
`inputs.input == "unterminated`,
`inputs.input == 'unterminated`,
`inputs.input == "mismatched'`,
```

- [ ] **Step 2: Run the focused test red**

Run: `go test ./internal/whenexpr -run '^TestConstitutionAllowsOnlySmallGateLanguage$' -count=1`

Expected: FAIL because the malformed expressions are currently accepted.

- [ ] **Step 3: Require matching outer quote delimiters**

Replace the first branch of `literal` with:

```go
if strings.ContainsAny(value, `"'`) {
	if len(value) < 2 || (value[0] != '"' && value[0] != '\'') || value[len(value)-1] != value[0] {
		return "", fmt.Errorf("quoted string literals must use matching delimiters")
	}
	return value[1 : len(value)-1], nil
}
```

Keep the existing whitespace and parentheses checks unchanged.

- [ ] **Step 4: Run the package tests green**

Run: `go test ./internal/whenexpr ./internal/workflow -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the `when` validation repair**

```bash
git add internal/whenexpr/whenexpr.go internal/whenexpr/whenexpr_test.go
git commit -m "fix: reject malformed when literals"
```

---

### Task 4: Require pinned TypeScript compilation in CI

**Files:**
- Modify: `internal/architecture/architecture_test.go`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `scripts/test-host-integrations-typescript.sh` and its existing `TAKT_REQUIRE_TYPESCRIPT`/`TSC` inputs.
- Produces: a CI release gate that cannot silently skip host-integration compilation.

- [ ] **Step 1: Add a failing architecture contract**

At the end of `TestShellSmokeBudget`, read `.github/workflows/ci.yml` and require:

```go
ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
if err != nil {
	t.Fatal(err)
}
for _, required := range []string{"actions/setup-node@v4", "npm install --global typescript@5.7.2", `TAKT_REQUIRE_TYPESCRIPT: "1"`} {
	if !strings.Contains(string(ci), required) {
		t.Errorf("CI TypeScript gate is missing %q", required)
	}
}
```

- [ ] **Step 2: Run the architecture test red**

Run: `go test ./internal/architecture -run '^TestShellSmokeBudget$' -count=1`

Expected: FAIL for all three missing CI requirements.

- [ ] **Step 3: Install and require the pinned compiler in GitHub Actions**

Add before the release-gate step:

```yaml
      - uses: actions/setup-node@v4
        with:
          node-version: 22
      - name: Install pinned TypeScript compiler
        run: npm install --global typescript@5.7.2
```

Add to the release-gate step:

```yaml
        env:
          TAKT_REQUIRE_TYPESCRIPT: "1"
```

- [ ] **Step 4: Run the architecture contract green**

Run: `go test ./internal/architecture -run '^TestShellSmokeBudget$' -count=1`

Expected: PASS.

- [ ] **Step 5: Exercise the required smoke with an isolated pinned compiler**

Run:

```bash
audit_ts=$(mktemp -d)
npm install --prefix "$audit_ts" --ignore-scripts --no-audit --no-fund typescript@5.7.2
TAKT_REQUIRE_TYPESCRIPT=1 TSC="$audit_ts/node_modules/.bin/tsc" ./scripts/test-host-integrations-typescript.sh
```

Expected: `coding-agent host integrations TypeScript: PASS`.

- [ ] **Step 6: Commit the CI enforcement**

```bash
git add .github/workflows/ci.yml internal/architecture/architecture_test.go
git commit -m "ci: require host TypeScript smoke"
```

---

### Task 5: Record and verify the repaired release state

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `docs/05-implementation-status.md`
- Modify: `TEST_RESULTS.md`
- Modify: `MANIFEST.sha256`
- Include: `docs/superpowers/specs/2026-08-10-release-gate-and-validation-repair-design.md`
- Include: `docs/superpowers/plans/2026-08-10-release-gate-and-validation-repair.md`

**Interfaces:**
- Consumes: the completed code/CI changes and their verification output.
- Produces: accurate release metadata and a manifest covering the final tracked tree.

- [ ] **Step 1: Update release documentation**

Add concise `v0.1.57-alpha` bullets stating:

```markdown
- Post-audit repair restored the clean-checkout Go module graph, single-document YAML/JSON authoring, and malformed quoted `when` rejection.
- GitHub Actions now installs pinned TypeScript 5.7.2 and requires host-integration compilation; local Go-only checks may still skip it when the compiler is absent.
```

Record the final commands and exact outcomes in a dated addendum at the top of `TEST_RESULTS.md`. Do not rewrite historical release evidence.

- [ ] **Step 2: Run formatting and focused checks**

Run:

```bash
gofmt -w internal
go vet ./...
go test ./internal/yamlcodec ./internal/whenexpr ./internal/workflow ./internal/architecture -count=1
```

Expected: PASS.

- [ ] **Step 3: Run full correctness and race gates**

Run:

```bash
go test ./... -count=1
go test -race ./... -count=1
```

Expected: PASS.

- [ ] **Step 4: Run docs, build, journeys, and external verification**

Run:

```bash
./scripts/check-docs.sh
go build ./cmd/takt
go test ./tests/e2e -run '^TestUserJourney' -count=1
```

Expected: PASS.

- [ ] **Step 5: Regenerate the release manifest mechanically**

Regenerate `MANIFEST.sha256` from every file except `.git/**`, `bin/**`, and the manifest itself, sorted by repository-relative path, using SHA-256. Then run:

```bash
./scripts/verify-manifest.sh
```

Expected: PASS with the new design and plan files included.

- [ ] **Step 6: Run the aggregate release gates**

Run:

```bash
make check
./scripts/verify.sh
```

Expected: PASS. The local TypeScript step may print `SKIP`; the separate required smoke from Task 4 must already have printed `PASS`.

- [ ] **Step 7: Commit release metadata and manifest**

```bash
git add CHANGELOG.md docs/05-implementation-status.md TEST_RESULTS.md MANIFEST.sha256 docs/superpowers
git commit -m "docs: record release gate repair"
```

- [ ] **Step 8: Confirm the final tree**

Run: `git status --short`

Expected: no output.
