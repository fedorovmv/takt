package application

import (
	"fmt"
	"sort"
	"strings"

	"takt/internal/blockcatalog"
	"takt/internal/profile"
)

type BlockCatalogView struct {
	Profile     string                        `json:"profile"`
	Packages    []blockcatalog.PackageSummary `json:"packages"`
	Blocks      []blockcatalog.ResolvedBlock  `json:"blocks"`
	Templates   map[string]string             `json:"templates,omitempty"`
	Governance  blockcatalog.Governance       `json:"governance,omitempty"`
	Fingerprint string                        `json:"fingerprint"`
}

func (s *CatalogService) ListBlocks(profileName string) (*BlockCatalogView, error) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		profileName = "code"
	}
	resolved, err := profile.Resolve(profileName, s.Workspace)
	if err != nil {
		return nil, err
	}
	catalog, err := catalogForResolved(resolved)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(catalog.Blocks))
	for name := range catalog.Blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	view := &BlockCatalogView{Profile: profileName, Packages: catalog.Packages, Templates: catalog.Templates, Governance: catalog.Governance, Fingerprint: catalog.Fingerprint}
	for _, name := range names {
		view.Blocks = append(view.Blocks, catalog.Blocks[name])
	}
	return view, nil
}

func (s *CatalogService) DescribeBlock(profileName, name string) (*blockcatalog.ResolvedBlock, error) {
	view, err := s.ListBlocks(profileName)
	if err != nil {
		return nil, err
	}
	for _, block := range view.Blocks {
		if block.Name == name {
			copy := block
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("trusted block %q was not found in profile %q", name, view.Profile)
}
