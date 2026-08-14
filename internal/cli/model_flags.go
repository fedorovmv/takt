package cli

import (
	"fmt"
	"os"
	"strings"
)

type modelOverrideFlag map[string]string

func (f *modelOverrideFlag) String() string { return "" }

func (f *modelOverrideFlag) Set(raw string) error {
	alias, value, ok := strings.Cut(raw, "=")
	if !ok || strings.TrimSpace(alias) != alias || alias == "" || value == "" {
		return fmt.Errorf("model override must be alias=provider/model")
	}
	if f == nil {
		return fmt.Errorf("model override target is nil")
	}
	if *f == nil {
		*f = modelOverrideFlag{}
	}
	(*f)[alias] = value
	return nil
}

func environmentModelOverrides(environ []string) (map[string]string, error) {
	values := map[string]string{}
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(name, "MODEL_") || value == "" {
			continue
		}
		alias := strings.ToLower(strings.TrimPrefix(name, "MODEL_"))
		if alias == "" {
			continue
		}
		target := modelOverrideFlag(values)
		if err := target.Set(alias + "=" + value); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func mergeModelOverrides(environment map[string]string, cli modelOverrideFlag) map[string]string {
	merged := make(map[string]string, len(environment)+len(cli))
	for alias, value := range environment {
		merged[alias] = value
	}
	for alias, value := range cli {
		merged[alias] = value
	}
	return merged
}

func currentEnvironmentModelOverrides() (map[string]string, error) {
	return environmentModelOverrides(os.Environ())
}
