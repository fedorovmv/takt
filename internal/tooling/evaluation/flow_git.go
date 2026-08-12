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

	"takt/internal/config"
	"takt/internal/profile"
	"takt/internal/spec"
)

var productionFlowModelSlots = map[string][]string{
	"code:feature-development":     {"implementation", "review"},
	"code:comprehensive-pr-review": {"review"},
	"code:architect":               {"implementation", "review", "routing"},
}

type PreparedFlowRepeat struct {
	CaseID, ControlWorkspace, BaselineWorkspace, ConfigPath, InputValue string
	ProfileFingerprint, HostPATHHash                                    string
	BaseCommit, HeadCommit, BareRemote                                  string
	Repeat                                                              int
}

func PrepareFlowRepeat(ctx context.Context, suite *FlowSuite, item FlowCase, repeat int, evidenceRoot, hostPath string) (*PreparedFlowRepeat, error) {
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
	if err := requireFlowModelSlots(selector, cfg.Models); err != nil {
		return nil, err
	}
	inputValue, err := flowInputValue(selector, item.InputPath, filepath.Join(control, ".takt", "eval", "input.md"))
	if err != nil {
		return nil, err
	}
	if err := initFlowGit(ctx, control); err != nil {
		return nil, err
	}
	head, err := runFlowGit(ctx, control, nil, "rev-parse", "HEAD")
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
		ProfileFingerprint: profileFingerprint,
		HostPATHHash:       hex.EncodeToString(pathHash[:]),
		BaseCommit:         strings.TrimSpace(head),
		HeadCommit:         strings.TrimSpace(head),
		Repeat:             repeat,
	}, nil
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

func requireFlowModelSlots(selector string, models map[string]spec.ModelSpec) error {
	for _, slot := range productionFlowModelSlots[selector] {
		if _, ok := models[slot]; !ok {
			return fmt.Errorf("workflow %q requires model slot %q", selector, slot)
		}
	}
	return nil
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
	return string(b), nil
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
	if err := json.Unmarshal(values["repository"], &repository); err != nil {
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

func initFlowGit(ctx context.Context, control string) error {
	if _, err := runFlowGit(ctx, control, nil, "init", "-b", "main"); err != nil {
		return err
	}
	if _, err := runFlowGit(ctx, control, nil, "config", "user.name", "Takt Eval"); err != nil {
		return err
	}
	if _, err := runFlowGit(ctx, control, nil, "config", "user.email", "eval@takt.invalid"); err != nil {
		return err
	}
	if _, err := runFlowGit(ctx, control, nil, "add", "--all"); err != nil {
		return err
	}
	dates := []string{"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z"}
	if _, err := runFlowGit(ctx, control, dates, "commit", "-m", "takt eval baseline"); err != nil {
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
