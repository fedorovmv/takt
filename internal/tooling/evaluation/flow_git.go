package evaluation

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"takt/internal/assistant"
	"takt/internal/config"
	"takt/internal/profile"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/workflow"
)

type PreparedFlowRepeat struct {
	CaseID, ControlWorkspace, BaselineWorkspace, ConfigPath, InputValue string
	ModelPreset                                                         string
	EffectiveModels                                                     map[string]string
	ProfileFingerprint, HostPATHHash                                    string
	BaseCommit, HeadCommit, BareRemote                                  string
	Repeat                                                              int
}

func PrepareFlowRepeat(ctx context.Context, suite *FlowSuite, item FlowCase, repeat int, evidenceRoot, hostPath string, selections ...config.ModelSelection) (*PreparedFlowRepeat, error) {
	if suite == nil {
		return nil, fmt.Errorf("nil suite")
	}
	if repeat < 1 {
		return nil, fmt.Errorf("repeat must be positive")
	}
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.WorkspacePath) == "" || strings.TrimSpace(item.InputPath) == "" {
		return nil, fmt.Errorf("flow case is incomplete")
	}
	root := filepath.Join(evidenceRoot, "workspaces", item.ID, fmt.Sprintf("repeat-%03d", repeat))
	control := filepath.Join(root, "control")
	baseline := filepath.Join(root, "baseline")
	if err := prepareFlowControl(suite, item, control); err != nil {
		return nil, err
	}
	selector := strings.TrimSpace(suite.Workflow)
	if suite.ResolvedWorkflow == "" {
		candidate := filepath.Join(control, selector)
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
			copy := *suite
			copy.ResolvedWorkflow = candidate
			suite = &copy
		}
	}
	profileName, _ := profile.SelectorParts(selector)
	if suite.ResolvedWorkflow == "" {
		if err := ensureFlowProfile(profileName, control); err != nil {
			return nil, err
		}
	}
	configPath := filepath.Join(control, ".takt", "config.yaml")
	if err := copyFile(suite.ResolvedConfig, configPath, 0o644); err != nil {
		return nil, fmt.Errorf("copy suite config: %w", err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	selection := config.ModelSelection{}
	if len(selections) > 0 {
		selection = selections[0]
	}
	effective, selectedPreset, err := config.MaterializeModels(cfg, selection)
	if err != nil {
		return nil, err
	}
	if err := writeFlowPiSettings(control, effective); err != nil {
		return nil, err
	}
	if err := validateFlowReferences(suite, selector, control, effective); err != nil {
		return nil, err
	}
	if suite.External.GitHub != nil {
		// Keep the source preset metadata in the copied config. Start rematerializes
		// the selected preset and must not receive an already-materialized config.
		if err := overlayFlowAssistantEnvironment(cfg, hostPath); err != nil {
			return nil, err
		}
		if err := writeFlowConfig(configPath, cfg); err != nil {
			return nil, err
		}
		if _, err := config.Load(configPath); err != nil {
			return nil, err
		}
	}
	inputValue, err := flowInputValue(selector, item.InputPath, filepath.Join(control, ".takt", "eval", "input.md"))
	if err != nil {
		return nil, err
	}
	base, head, bareRemote, err := prepareFlowSCM(ctx, suite, item, control, root)
	if err != nil {
		return nil, err
	}
	if err := archiveFlowGit(ctx, control, baseline); err != nil {
		return nil, err
	}
	profileFingerprint := ""
	if suite.ResolvedWorkflow == "" {
		profileFingerprint, err = hashPath(filepath.Join(control, ".takt", "profiles", profileName))
		if err != nil {
			return nil, err
		}
	}
	pathHash := sha256.Sum256([]byte(hostPath))
	return &PreparedFlowRepeat{
		CaseID:             item.ID,
		ControlWorkspace:   control,
		BaselineWorkspace:  baseline,
		ConfigPath:         configPath,
		InputValue:         inputValue,
		ModelPreset:        selectedPreset,
		EffectiveModels:    modelReferences(effective.Models),
		ProfileFingerprint: profileFingerprint,
		HostPATHHash:       hex.EncodeToString(pathHash[:]),
		BaseCommit:         base,
		HeadCommit:         head,
		BareRemote:         bareRemote,
		Repeat:             repeat,
	}, nil
}

func writeFlowPiSettings(workspace string, cfg *spec.Config) error {
	pi, ok := flowPiAssistant(cfg)
	if !ok {
		return nil
	}
	path := filepath.Join(workspace, ".pi", "settings.json")
	settings := map[string]any{}
	if pi.Settings != nil {
		data, err := json.Marshal(pi.Settings)
		if err != nil {
			return fmt.Errorf("encode configured Pi settings: %w", err)
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("copy configured Pi settings: %w", err)
		}
	}
	retry, ok := settings["retry"].(map[string]any)
	if !ok {
		if settings["retry"] != nil {
			return fmt.Errorf("eval Pi retry settings must be an object")
		}
		retry = map[string]any{}
		settings["retry"] = retry
	}
	retry["enabled"] = false
	provider, ok := retry["provider"].(map[string]any)
	if !ok {
		if retry["provider"] != nil {
			return fmt.Errorf("eval Pi provider retry settings must be an object")
		}
		provider = map[string]any{}
		retry["provider"] = provider
	}
	provider["maxRetries"] = 0
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return writeFlowRaw(path, append(data, '\n'), 0o644)
}

func flowPiAssistant(cfg *spec.Config) (spec.AssistantSpec, bool) {
	_, configured, ok := assistant.ResolveConfigured("coding-agent", cfg)
	return configured, ok && configured.Type == "pi"
}

func modelReferences(models map[string]spec.ModelSpec) map[string]string {
	if len(models) == 0 {
		return nil
	}
	values := make(map[string]string, len(models))
	for name, model := range models {
		values[name] = model.Provider + "/" + model.ID
	}
	return values
}

func prepareFlowControl(suite *FlowSuite, item FlowCase, control string) error {
	if err := CopyFlowCaseWorkspace(item.WorkspacePath, control); err != nil {
		return fmt.Errorf("copy case workspace: %w", err)
	}
	return copyFile(item.InputPath, filepath.Join(control, ".takt", "eval", "input.md"), 0o644)
}

func ensureFlowProfile(name, control string) error {
	manifest := filepath.Join(control, ".takt", "profiles", name, "profile.yaml")
	if info, err := os.Stat(manifest); err == nil && info.Mode().IsRegular() {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !profile.IsBuiltin(name) {
		return fmt.Errorf("profile %q is not installed in evaluation workspace", name)
	}
	_, err := profile.Init(name, control, false)
	return err
}

func validateFlowReferences(suite *FlowSuite, selector, control string, cfg *spec.Config) error {
	path := suite.ResolvedWorkflow
	if path == "" {
		resolved, err := profile.Resolve(selector, control)
		if err != nil {
			return err
		}
		path = resolved.WorkflowPath
	}
	wf, err := workflow.Load(path)
	if err != nil {
		return err
	}
	if wf.Model != "" {
		if _, ok := cfg.Models[wf.Model]; !ok {
			return fmt.Errorf("workflow %q requires model alias %q", selector, wf.Model)
		}
	}
	return workflow.ValidateReferences(wf, cfg, runtime.NewCommandResolver(path, control, control))
}

func flowInputValue(selector, source, copied string) (string, error) {
	if selector != "code:comprehensive-pr-review" && selector != "code:architect" {
		return copied, nil
	}
	b, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	if err := validateFlowPRInput(b, selector == "code:comprehensive-pr-review"); err != nil {
		return "", err
	}
	return copied, nil
}

func validateFlowPRInput(input []byte, comprehensive bool) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	var values map[string]json.RawMessage
	if err := decoder.Decode(&values); err != nil || values == nil {
		return fmt.Errorf("workflow input must be one JSON object")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return fmt.Errorf("workflow input must be one JSON object")
	}
	var repository string
	var pullRequest int
	var fixesPermitted bool
	repositoryValue, ok := values["repository"]
	if !ok || bytes.Equal(bytes.TrimSpace(repositoryValue), []byte("null")) || json.Unmarshal(repositoryValue, &repository) != nil {
		return fmt.Errorf("workflow input repository must be a string")
	}
	if err := json.Unmarshal(values["pull_request"], &pullRequest); err != nil || pullRequest <= 0 {
		return fmt.Errorf("workflow input pull_request must be a positive integer")
	}
	fixesPermittedValue, ok := values["fixes_permitted"]
	if !ok || bytes.Equal(bytes.TrimSpace(fixesPermittedValue), []byte("null")) || json.Unmarshal(fixesPermittedValue, &fixesPermitted) != nil {
		return fmt.Errorf("workflow input fixes_permitted must be a boolean")
	}
	if !comprehensive {
		return nil
	}
	var commands []string
	if err := json.Unmarshal(values["validation_commands"], &commands); err != nil || len(commands) == 0 {
		return fmt.Errorf("workflow input validation_commands must be a non-empty array")
	}
	seen := map[string]bool{}
	for _, command := range commands {
		if strings.TrimSpace(command) == "" || seen[command] {
			return fmt.Errorf("workflow input validation_commands must be unique non-empty strings")
		}
		seen[command] = true
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("extra JSON value")
	}
	return nil
}

func prepareFlowSCM(ctx context.Context, suite *FlowSuite, item FlowCase, control, repeatRoot string) (string, string, string, error) {
	if suite.External.GitHub == nil {
		if err := initFlowGit(ctx, control, "main"); err != nil {
			return "", "", "", err
		}
		if err := commitFlowBaseline(ctx, control, "takt eval baseline"); err != nil {
			return "", "", "", err
		}
		head, err := runFlowGit(ctx, control, nil, "rev-parse", "HEAD")
		return strings.TrimSpace(head), strings.TrimSpace(head), "", err
	}
	if suite.External.GitHub.Mode != "fixture" {
		return "", "", "", fmt.Errorf("unsupported github fixture mode")
	}
	fixture, err := LoadFlowSCMFixture(item.Root, suite.External.GitHub.Require)
	if err != nil {
		return "", "", "", err
	}
	if err := writeFlowGHFixture(control, fixture); err != nil {
		return "", "", "", err
	}
	if err := initFlowGit(ctx, control, fixture.Repository.BaseBranch); err != nil {
		return "", "", "", err
	}
	if err := commitFlowBaseline(ctx, control, "takt eval base"); err != nil {
		return "", "", "", err
	}
	base, err := runFlowGit(ctx, control, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", "", "", err
	}
	if _, err := runFlowGit(ctx, control, nil, "checkout", "-b", fixture.Repository.HeadBranch); err != nil {
		return "", "", "", err
	}
	if fixture.PullRequest != nil {
		if _, err := runFlowGit(ctx, control, nil, "apply", "--check", "--whitespace=error-all", filepath.Join(item.Root, "scm", "head.patch")); err != nil {
			return "", "", "", err
		}
		if _, err := runFlowGit(ctx, control, nil, "apply", "--whitespace=error-all", filepath.Join(item.Root, "scm", "head.patch")); err != nil {
			return "", "", "", err
		}
		if err := commitFlowBaseline(ctx, control, "takt eval pull request head"); err != nil {
			return "", "", "", err
		}
	}
	head, err := runFlowGit(ctx, control, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", "", "", err
	}
	bareRemote := filepath.Join(repeatRoot, "origin.git")
	if _, err := runFlowGit(ctx, control, nil, "init", "--bare", bareRemote); err != nil {
		return "", "", "", err
	}
	if _, err := runFlowGit(ctx, control, nil, "remote", "add", "origin", bareRemote); err != nil {
		return "", "", "", err
	}
	if _, err := runFlowGit(ctx, control, nil, "push", "origin", fixture.Repository.BaseBranch+":refs/heads/"+fixture.Repository.BaseBranch, fixture.Repository.HeadBranch+":refs/heads/"+fixture.Repository.HeadBranch); err != nil {
		return "", "", "", err
	}
	if _, err := runFlowGit(ctx, control, nil, "checkout", fixture.Repository.HeadBranch); err != nil {
		return "", "", "", err
	}
	if status, err := runFlowGit(ctx, control, nil, "status", "--porcelain"); err != nil {
		return "", "", "", err
	} else if status != "" {
		return "", "", "", fmt.Errorf("initial evaluation workspace is dirty: %s", status)
	}
	return strings.TrimSpace(base), strings.TrimSpace(head), bareRemote, nil
}

func overlayFlowAssistantEnvironment(cfg *spec.Config, hostPath string) error {
	values := map[string]string{
		"PATH":                "{{workspace}}/.takt/eval/bin:" + hostPath,
		"FAKE_GH_FIXTURE_DIR": "{{workspace}}/.takt/eval/scm-fixture",
		"FAKE_GH_STATE_DIR":   "{{workspace}}/.takt/evals/scm",
	}
	for name, assistant := range cfg.Assistants {
		env := make(map[string]string, len(assistant.Env)+len(values))
		for key, value := range assistant.Env {
			env[key] = value
		}
		for key, value := range values {
			if current, exists := env[key]; exists && current != value {
				return fmt.Errorf("assistant %q env %q conflicts with flow evaluation fixture", name, key)
			}
			env[key] = value
		}
		assistant.Env = env
		cfg.Assistants[name] = assistant
	}
	return nil
}

func writeFlowConfig(path string, cfg *spec.Config) error {
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func initFlowGit(ctx context.Context, control, branch string) error {
	if _, err := runFlowGit(ctx, control, nil, "init", "-b", branch); err != nil {
		return err
	}
	if _, err := runFlowGit(ctx, control, nil, "config", "user.name", "Takt Eval"); err != nil {
		return err
	}
	if _, err := runFlowGit(ctx, control, nil, "config", "user.email", "eval@takt.invalid"); err != nil {
		return err
	}
	return nil
}

func commitFlowBaseline(ctx context.Context, control, message string) error {
	if _, err := runFlowGit(ctx, control, nil, "add", "--all"); err != nil {
		return err
	}
	dates := []string{"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
	if _, err := runFlowGit(ctx, control, dates, "commit", "-m", message); err != nil {
		return err
	}
	status, err := runFlowGit(ctx, control, nil, "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("initial evaluation workspace is dirty: %s", status)
	}
	return nil
}

func archiveFlowGit(ctx context.Context, control, baseline string) error {
	if err := os.MkdirAll(baseline, 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", control, "archive", "--format=tar", "HEAD")
	archive, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git archive: %w", err)
	}
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(baseline, filepath.FromSlash(header.Name))
		if filepath.Clean(target) == baseline || !strings.HasPrefix(filepath.Clean(target), baseline+string(filepath.Separator)) {
			return fmt.Errorf("invalid git archive path %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader:
			continue
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported git archive entry %q", header.Name)
		}
	}
}

func runFlowGit(ctx context.Context, control string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", control}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}
