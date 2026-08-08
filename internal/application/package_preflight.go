package application

import (
	"context"
	"fmt"
	"sort"

	"takt/internal/blockcatalog"
	"takt/internal/domainadapter"
	"takt/internal/spec"
)

type AdapterPreflightStatus struct {
	Name              string   `json:"name"`
	Domain            string   `json:"domain"`
	Level             string   `json:"level"`
	Available         bool     `json:"available"`
	MissingOperations []string `json:"missing_operations,omitempty"`
	MissingReconcile  []string `json:"missing_reconcile,omitempty"`
	Error             string   `json:"error,omitempty"`
}

func preflightCatalogAdapters(ctx context.Context, catalog *blockcatalog.Catalog, cfg *spec.Config) ([]AdapterPreflightStatus, error) {
	if catalog == nil || len(catalog.Requirements.Adapters) == 0 {
		return nil, nil
	}
	statuses := make([]AdapterPreflightStatus, 0, len(catalog.Requirements.Adapters))
	for _, req := range catalog.Requirements.Adapters {
		level := req.Level
		if level == "" {
			level = "required"
		}
		st := AdapterPreflightStatus{Name: req.Name, Domain: req.Domain, Level: level}
		specValue, ok := cfg.Adapters[req.Name]
		if !ok {
			st.Error = "adapter is not configured"
		} else if specValue.Domain != req.Domain {
			st.Error = fmt.Sprintf("configured domain %s differs from required %s", specValue.Domain, req.Domain)
		} else {
			adapter, err := (domainadapter.Factory{Config: cfg}).Resolve(req.Name)
			if err != nil {
				st.Error = err.Error()
			} else {
				decl, err := adapter.Describe(ctx)
				if err != nil {
					st.Error = err.Error()
				} else if err := domainadapter.ValidateDeclaration(decl); err != nil {
					st.Error = err.Error()
				} else {
					caps, recs := map[string]bool{}, map[string]bool{}
					for _, op := range decl.Capabilities {
						caps[op] = true
					}
					for _, op := range decl.Reconcile {
						recs[op] = true
					}
					for _, op := range req.Operations {
						if !caps[op] {
							st.MissingOperations = append(st.MissingOperations, op)
						}
					}
					for _, op := range req.Reconcile {
						if !recs[op] {
							st.MissingReconcile = append(st.MissingReconcile, op)
						}
					}
					sort.Strings(st.MissingOperations)
					sort.Strings(st.MissingReconcile)
					st.Available = len(st.MissingOperations) == 0 && len(st.MissingReconcile) == 0
				}
			}
		}
		statuses = append(statuses, st)
		if level == "required" && !st.Available {
			return statuses, fmt.Errorf("required package adapter %s is unavailable: %s missing_operations=%v missing_reconcile=%v", req.Name, st.Error, st.MissingOperations, st.MissingReconcile)
		}
	}
	return statuses, nil
}

// PreflightCatalogAdapters exposes package adapter capability checks to local
// operator commands such as `takt package doctor`. Runtime planning uses the
// same implementation, so diagnostics and execution cannot drift.
func PreflightCatalogAdapters(ctx context.Context, catalog *blockcatalog.Catalog, cfg *spec.Config) ([]AdapterPreflightStatus, error) {
	return preflightCatalogAdapters(ctx, catalog, cfg)
}
