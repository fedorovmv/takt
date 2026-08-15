package evaluation

import "time"

type AnalysisCase struct {
	CaseID string `json:"case_id"`
	Repeat int    `json:"repeat"`
}

type AnalysisCaseRef struct {
	CaseID string `json:"case_id"`
	Repeat int    `json:"repeat"`
}

type AnalysisDeterministic struct {
	Status      string `json:"status,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	CauseSource string `json:"cause_source,omitempty"`
	Cause       string `json:"cause,omitempty"`
}

type AnalysisModel struct {
	Preset   string `json:"preset"`
	Alias    string `json:"alias"`
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

type AnalysisSession struct {
	Adapter             string `json:"adapter"`
	SessionID           string `json:"session_id"`
	SessionPath         string `json:"session_path,omitempty"`
	SessionEvidence     string `json:"session_evidence"`
	SessionEvidencePath string `json:"session_evidence_path,omitempty"`
}

type AnalysisUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
	DurationMS   int64   `json:"duration_ms"`
}

type AdvisoryCausalLink struct {
	Fact        string   `json:"fact"`
	Consequence string   `json:"consequence"`
	Evidence    []string `json:"evidence"`
}
type AdvisoryEvidence struct {
	Path    string `json:"path"`
	Pointer string `json:"pointer"`
	Fact    string `json:"fact"`
}
type AdvisoryDisagreement struct {
	WithDeterministicCause bool   `json:"with_deterministic_cause"`
	Explanation            string `json:"explanation"`
}
type AdvisoryAnalysis struct {
	PrimaryClass        string               `json:"primary_class"`
	FailureMode         string               `json:"failure_mode"`
	Confidence          string               `json:"confidence"`
	RootCause           string               `json:"root_cause"`
	CausalChain         []AdvisoryCausalLink `json:"causal_chain"`
	Evidence            []AdvisoryEvidence   `json:"evidence"`
	ContributingFactors []string             `json:"contributing_factors"`
	RecommendedActions  []string             `json:"recommended_actions"`
	MissingEvidence     []string             `json:"missing_evidence"`
	Disagreement        AdvisoryDisagreement `json:"disagreement"`
}

type AnalysisCaseReport struct {
	CaseID              string                `json:"case_id"`
	Repeat              int                   `json:"repeat"`
	Deterministic       AnalysisDeterministic `json:"deterministic"`
	AnalysisStatus      string                `json:"analysis_status"`
	Analysis            *AdvisoryAnalysis     `json:"analysis,omitempty"`
	EvidenceFingerprint string                `json:"evidence_fingerprint"`
	Model               AnalysisModel         `json:"model"`
	Session             AnalysisSession       `json:"session"`
	Usage               AnalysisUsage         `json:"usage"`
	ErrorCode           string                `json:"error_code,omitempty"`
	Error               string                `json:"error,omitempty"`
}

// AnalysisRunReport is the durable report for one analysis invocation.
type AnalysisRunReport struct {
	ReportVersion       string               `json:"report_version"`
	OutputDir           string               `json:"output_dir"`
	SourceEvaluationDir string               `json:"source_evaluation_dir"`
	Status              string               `json:"status"`
	StartedAt           time.Time            `json:"started_at"`
	FinishedAt          time.Time            `json:"finished_at"`
	DurationMS          int64                `json:"duration_ms"`
	Model               AnalysisModel        `json:"model"`
	SelectedCases       []AnalysisCaseRef    `json:"selected_cases"`
	Analyses            []AnalysisCaseReport `json:"analyses"`
}

type CaseReport = AnalysisCaseReport
type RunReport = AnalysisRunReport
type CausalLink = AdvisoryCausalLink
type Evidence = AdvisoryEvidence
type Disagreement = AdvisoryDisagreement
