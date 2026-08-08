package application

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"takt/internal/blockcatalog"
	"takt/internal/spec"
)

func TestPackageAdapterPreflightRequiredAndPreferred(t *testing.T) {
	cfg := &spec.Config{Adapters: map[string]spec.DomainAdapterSpec{"scm": {Domain: "scm", Transport: "process", Argv: []string{os.Args[0], "-test.run=TestPackageAdapterPreflightHelper"}, Env: map[string]string{"TAKT_PREFLIGHT_HELPER": "1"}}}}
	required := &blockcatalog.Catalog{Requirements: blockcatalog.Requirements{Adapters: []blockcatalog.AdapterRequirement{{Name: "scm", Domain: "scm", Operations: []string{"change.create"}, Level: "required"}}}}
	statuses, err := preflightCatalogAdapters(context.Background(), required, cfg, testAdapterFactory(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || !statuses[0].Available {
		t.Fatalf("statuses=%+v", statuses)
	}
	missing := &blockcatalog.Catalog{Requirements: blockcatalog.Requirements{Adapters: []blockcatalog.AdapterRequirement{{Name: "scm", Domain: "scm", Operations: []string{"change.review"}, Level: "required"}}}}
	if _, err := preflightCatalogAdapters(context.Background(), missing, cfg, testAdapterFactory(cfg)); err == nil {
		t.Fatal("expected required capability failure")
	}
	missing.Requirements.Adapters[0].Level = "preferred"
	statuses, err = preflightCatalogAdapters(context.Background(), missing, cfg, testAdapterFactory(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].Available || len(statuses[0].MissingOperations) != 1 {
		t.Fatalf("statuses=%+v", statuses)
	}
}

func TestPackageAdapterPreflightHelper(t *testing.T) {
	if os.Getenv("TAKT_PREFLIGHT_HELPER") == "" {
		return
	}
	var request map[string]any
	if json.NewDecoder(os.Stdin).Decode(&request) != nil {
		os.Exit(2)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"apiVersion": "takt-domain-adapter/v1alpha1", "kind": "DescribeResponse", "declaration": map[string]any{"apiVersion": "takt-domain-adapter/v1alpha1", "kind": "AdapterCapabilities", "domain": "scm", "capabilities": []string{"change.create"}}})
	os.Exit(0)
}
