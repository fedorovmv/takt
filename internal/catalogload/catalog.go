package catalogload

import (
	"fmt"

	"takt/internal/extensions/blockcatalog"
	"takt/internal/extensions/packagedist"
	"takt/internal/profile"
)

// Paths returns the trusted block package manifests visible to an extension-aware
// consumer. Stable profile resolution intentionally knows only packages declared
// by the profile itself; installed packages are an extension concern.
func Paths(resolved *profile.Resolved, workspace string) ([]string, error) {
	if resolved == nil {
		return nil, fmt.Errorf("resolved profile is required")
	}
	paths := append([]string(nil), resolved.BlockPackagePaths...)
	installed, err := packagedist.InstalledManifestPaths(workspace)
	if err != nil {
		return nil, err
	}
	return append(paths, installed...), nil
}

func FromResolved(resolved *profile.Resolved, workspace string) (*blockcatalog.Catalog, error) {
	paths, err := Paths(resolved, workspace)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("profile %q does not declare trusted block packages", resolved.Name)
	}
	return blockcatalog.Load(paths)
}
