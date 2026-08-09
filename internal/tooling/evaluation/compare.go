package evaluation

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const CompareReportVersion = "takt-evaluation-compare/v1alpha1"

type MetricComparison struct {
	Baseline     *float64 `json:"baseline"`
	Candidate    *float64 `json:"candidate"`
	Delta        *float64 `json:"delta"`
	DeltaPercent *float64 `json:"delta_percent"`
	DeltaPP      *float64 `json:"delta_percentage_points,omitempty"`
}

type PairedOutcomeSummary struct {
	BothValid          int `json:"both_valid"`
	BaselineOnlyValid  int `json:"baseline_only_valid"`
	CandidateOnlyValid int `json:"candidate_only_valid"`
	BothInvalid        int `json:"both_invalid"`
}

type CaseComparison struct {
	CaseID                 string            `json:"case_id"`
	Repeat                 int               `json:"repeat"`
	Labels                 map[string]string `json:"labels,omitempty"`
	BaselineValid          bool              `json:"baseline_valid"`
	CandidateValid         bool              `json:"candidate_valid"`
	Transition             string            `json:"transition"`
	BaselineTimeToValidMS  *int64            `json:"baseline_time_to_valid_ms"`
	CandidateTimeToValidMS *int64            `json:"candidate_time_to_valid_ms"`
}

type CategoryComparison struct {
	Category string               `json:"category"`
	Total    int                  `json:"total"`
	Outcomes PairedOutcomeSummary `json:"outcomes"`
}

type CompareMetrics struct {
	SuccessAt1         MetricComparison `json:"success_at_1"`
	FinalSuccessRate   MetricComparison `json:"final_success_rate"`
	AverageAttempts    MetricComparison `json:"average_attempts_to_valid"`
	AverageScore       MetricComparison `json:"average_score"`
	CostPerValid       MetricComparison `json:"cost_per_valid"`
	AverageTimeToValid MetricComparison `json:"average_time_to_valid_ms"`
}

type CompareReport struct {
	ReportVersion string               `json:"report_version"`
	Benchmark     BenchmarkIdentity    `json:"benchmark"`
	Baseline      StrategyIdentity     `json:"baseline"`
	Candidate     StrategyIdentity     `json:"candidate"`
	Metrics       CompareMetrics       `json:"metrics"`
	Outcomes      PairedOutcomeSummary `json:"paired_outcomes"`
	Cases         []CaseComparison     `json:"cases"`
	ByCategory    []CategoryComparison `json:"by_category,omitempty"`
}

func Compare(baseline, candidate *SuiteReport) (*CompareReport, error) {
	if baseline == nil || candidate == nil {
		return nil, fmt.Errorf("baseline and candidate reports are required")
	}
	if baseline.Benchmark.Fingerprint == "" || candidate.Benchmark.Fingerprint == "" || baseline.Benchmark.Fingerprint != candidate.Benchmark.Fingerprint {
		return nil, fmt.Errorf("benchmark fingerprints differ: baseline=%q candidate=%q", baseline.Benchmark.Fingerprint, candidate.Benchmark.Fingerprint)
	}
	out := &CompareReport{
		ReportVersion: CompareReportVersion,
		Benchmark:     baseline.Benchmark,
		Baseline:      baseline.Strategy,
		Candidate:     candidate.Strategy,
		Metrics: CompareMetrics{
			SuccessAt1:         compareMetric(baseline.Summary.SuccessAt1, candidate.Summary.SuccessAt1, true),
			FinalSuccessRate:   compareMetric(baseline.Summary.FinalSuccessRate, candidate.Summary.FinalSuccessRate, true),
			AverageAttempts:    compareMetric(baseline.Summary.AverageAttemptsToValid, candidate.Summary.AverageAttemptsToValid, false),
			AverageScore:       compareMetric(baseline.Summary.AverageScore, candidate.Summary.AverageScore, false),
			CostPerValid:       compareMetric(baseline.Summary.CostPerValid, candidate.Summary.CostPerValid, false),
			AverageTimeToValid: compareMetric(baseline.Summary.AverageTimeToValidMS, candidate.Summary.AverageTimeToValidMS, false),
		},
	}
	baseRuns := indexRuns(baseline.Runs)
	candidateRuns := indexRuns(candidate.Runs)
	if len(baseRuns) != len(candidateRuns) {
		return nil, fmt.Errorf("paired run count differs: baseline=%d candidate=%d", len(baseRuns), len(candidateRuns))
	}
	keys := make([]string, 0, len(baseRuns))
	for key := range baseRuns {
		if _, ok := candidateRuns[key]; !ok {
			return nil, fmt.Errorf("candidate report is missing paired run %s", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	categories := map[string]*CategoryComparison{}
	for _, key := range keys {
		base := baseRuns[key]
		cand := candidateRuns[key]
		baseValid, candValid := qualitySucceeded(base), qualitySucceeded(cand)
		transition := transitionName(baseValid, candValid)
		item := CaseComparison{
			CaseID: base.CaseID, Repeat: base.Repeat, Labels: cloneLabels(base.Labels),
			BaselineValid: baseValid, CandidateValid: candValid, Transition: transition,
			BaselineTimeToValidMS: base.TimeToValidMS, CandidateTimeToValidMS: cand.TimeToValidMS,
		}
		out.Cases = append(out.Cases, item)
		addOutcome(&out.Outcomes, baseValid, candValid)
		category := base.Labels["category"]
		if category == "" {
			category = "unclassified"
		}
		entry := categories[category]
		if entry == nil {
			entry = &CategoryComparison{Category: category}
			categories[category] = entry
		}
		entry.Total++
		addOutcome(&entry.Outcomes, baseValid, candValid)
	}
	categoryNames := make([]string, 0, len(categories))
	for name := range categories {
		categoryNames = append(categoryNames, name)
	}
	sort.Strings(categoryNames)
	for _, name := range categoryNames {
		out.ByCategory = append(out.ByCategory, *categories[name])
	}
	return out, nil
}

func compareMetric(baseline, candidate *float64, percentagePoints bool) MetricComparison {
	out := MetricComparison{Baseline: baseline, Candidate: candidate}
	if baseline == nil || candidate == nil {
		return out
	}
	delta := *candidate - *baseline
	out.Delta = floatPointer(delta)
	if *baseline != 0 {
		out.DeltaPercent = floatPointer(delta / math.Abs(*baseline) * 100)
	}
	if percentagePoints {
		out.DeltaPP = floatPointer(delta * 100)
	}
	return out
}

func indexRuns(records []RunRecord) map[string]RunRecord {
	out := make(map[string]RunRecord, len(records))
	for _, record := range records {
		out[fmt.Sprintf("%s#%06d", record.CaseID, record.Repeat)] = record
	}
	return out
}

func transitionName(baselineValid, candidateValid bool) string {
	switch {
	case baselineValid && candidateValid:
		return "both_valid"
	case baselineValid:
		return "baseline_only_valid"
	case candidateValid:
		return "candidate_only_valid"
	default:
		return "both_invalid"
	}
}

func addOutcome(summary *PairedOutcomeSummary, baselineValid, candidateValid bool) {
	switch transitionName(baselineValid, candidateValid) {
	case "both_valid":
		summary.BothValid++
	case "baseline_only_valid":
		summary.BaselineOnlyValid++
	case "candidate_only_valid":
		summary.CandidateOnlyValid++
	case "both_invalid":
		summary.BothInvalid++
	}
}

func cloneLabels(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func (r CompareReport) String() string {
	lines := []string{
		fmt.Sprintf("benchmark: %s", r.Benchmark.ID),
		fmt.Sprintf("baseline: %s", r.Baseline.ID),
		fmt.Sprintf("candidate: %s", r.Candidate.ID),
		"metric\tbaseline\tcandidate\tdelta",
		fmt.Sprintf("success@1\t%s\t%s\t%s pp", formatMetric(r.Metrics.SuccessAt1.Baseline), formatMetric(r.Metrics.SuccessAt1.Candidate), formatMetric(r.Metrics.SuccessAt1.DeltaPP)),
		fmt.Sprintf("final success\t%s\t%s\t%s pp", formatMetric(r.Metrics.FinalSuccessRate.Baseline), formatMetric(r.Metrics.FinalSuccessRate.Candidate), formatMetric(r.Metrics.FinalSuccessRate.DeltaPP)),
		fmt.Sprintf("attempts/valid\t%s\t%s\t%s", formatMetric(r.Metrics.AverageAttempts.Baseline), formatMetric(r.Metrics.AverageAttempts.Candidate), formatMetric(r.Metrics.AverageAttempts.Delta)),
		fmt.Sprintf("cost/valid\t%s\t%s\t%s%%", formatMetric(r.Metrics.CostPerValid.Baseline), formatMetric(r.Metrics.CostPerValid.Candidate), formatMetric(r.Metrics.CostPerValid.DeltaPercent)),
		fmt.Sprintf("time-to-valid ms\t%s\t%s\t%s%%", formatMetric(r.Metrics.AverageTimeToValid.Baseline), formatMetric(r.Metrics.AverageTimeToValid.Candidate), formatMetric(r.Metrics.AverageTimeToValid.DeltaPercent)),
		fmt.Sprintf("paired outcomes: both_valid=%d candidate_only=%d baseline_only=%d both_invalid=%d", r.Outcomes.BothValid, r.Outcomes.CandidateOnlyValid, r.Outcomes.BaselineOnlyValid, r.Outcomes.BothInvalid),
	}
	return strings.Join(lines, "\n")
}
