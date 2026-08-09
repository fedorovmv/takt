package packagedist

import "time"

const (
	LockAPIVersion = "takt/v1alpha1"
	LockKind       = "PackageLock"
	PolicyKind     = "PackagePolicy"
	SignatureKind  = "PackageSignature"
)

type Source struct {
	Type     string `json:"type"` // local | git
	Location string `json:"location"`
	Ref      string `json:"ref,omitempty"`
	Commit   string `json:"commit,omitempty"`
}

type LockedPackage struct {
	Name              string    `json:"name"`
	Version           string    `json:"version"`
	Scope             string    `json:"scope"`
	Source            Source    `json:"source"`
	Checksum          string    `json:"checksum"`
	SignatureKeyID    string    `json:"signature_key_id,omitempty"`
	SignatureVerified bool      `json:"signature_verified,omitempty"`
	InstalledAt       time.Time `json:"installed_at"`
}

type Lock struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Packages   []LockedPackage `json:"packages"`
}

type Policy struct {
	APIVersion             string            `json:"apiVersion"`
	Kind                   string            `json:"kind"`
	AllowedSources         []string          `json:"allowed_sources,omitempty"`
	RequireSignatureScopes []string          `json:"require_signature_scopes,omitempty"`
	TrustedKeys            map[string]string `json:"trusted_keys,omitempty"` // base64 Ed25519 public keys
}

type Signature struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	KeyID      string `json:"key_id"`
	Algorithm  string `json:"algorithm"`
	Digest     string `json:"digest"`
	Signature  string `json:"signature"`
}

type InstallOptions struct{ Scope, Ref string }

type DoctorItem struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Scope     string   `json:"scope"`
	Status    string   `json:"status"`
	Checksum  string   `json:"checksum,omitempty"`
	Signature string   `json:"signature,omitempty"`
	Problems  []string `json:"problems,omitempty"`
}

type DoctorReport struct {
	Status   string       `json:"status"`
	Packages []DoctorItem `json:"packages"`
}
