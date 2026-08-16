package evaluation

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
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
	BaselineOutcome        *string           `json:"baseline_outcome"`
	CandidateOutcome       *string           `json:"candidate_outcome"`
}

type CategoryComparison struct {
	Category string               `json:"category"`
	Total    int                  `json:"total"`
	Outcomes PairedOutcomeSummary `json:"outcomes"`
}

type CompareMetrics struct {
	SuccessAt1         MetricComparison    `json:"success_at_1"`
	FinalSuccessRate   MetricComparison    `json:"final_success_rate"`
	InputTokens        MetricComparison    `json:"input_tokens"`
	OutputTokens       MetricComparison    `json:"output_tokens"`
	TotalTokens        MetricComparison    `json:"total_tokens"`
	TotalAttempts      MetricComparison    `json:"total_attempts"`
	TotalDurationMS    MetricComparison    `json:"total_duration_ms"`
	AverageAttempts    MetricComparison    `json:"average_attempts_to_valid"`
	AverageScore       MetricComparison    `json:"average_score"`
	CostPerValid       MetricComparison    `json:"cost_per_valid"`
	AverageTimeToValid MetricComparison    `json:"average_time_to_valid_ms"`
	Flow               *FlowCompareMetrics `json:"flow,omitempty"`
}

type FlowCompareMetrics struct {
	ValidRate           MetricComparison `json:"valid_rate"`
	FalseAcceptRate     MetricComparison `json:"false_accept_rate"`
	FalseRejectRate     MetricComparison `json:"false_reject_rate"`
	FlowCompletionRate  MetricComparison `json:"flow_completion_rate"`
	ValidationErrorRate MetricComparison `json:"validation_error_rate"`
}

type CompareReport struct {
	ReportVersion      string               `json:"report_version"`
	Benchmark          BenchmarkIdentity    `json:"benchmark"`
	Baseline           StrategyIdentity     `json:"baseline"`
	Candidate          StrategyIdentity     `json:"candidate"`
	BaselineOutputDir  string               `json:"baseline_output_dir"`
	CandidateOutputDir string               `json:"candidate_output_dir"`
	Metrics            CompareMetrics       `json:"metrics"`
	Outcomes           PairedOutcomeSummary `json:"paired_outcomes"`
	Cases              []CaseComparison     `json:"cases"`
	ByCategory         []CategoryComparison `json:"by_category,omitempty"`
}

func Compare(baseline, candidate *SuiteReport) (*CompareReport, error) {
	if baseline == nil || candidate == nil {
		return nil, fmt.Errorf("baseline and candidate reports are required")
	}
	if baseline.Benchmark.Fingerprint == "" || candidate.Benchmark.Fingerprint == "" || baseline.Benchmark.Fingerprint != candidate.Benchmark.Fingerprint {
		return nil, fmt.Errorf("benchmark fingerprints differ: baseline=%q candidate=%q", baseline.Benchmark.Fingerprint, candidate.Benchmark.Fingerprint)
	}
	if (baseline.Mode == "flow") != (candidate.Mode == "flow") {
		return nil, fmt.Errorf("cannot compare flow and workflow evaluation reports")
	}
	out := &CompareReport{
		ReportVersion:      CompareReportVersion,
		Benchmark:          baseline.Benchmark,
		Baseline:           baseline.Strategy,
		Candidate:          candidate.Strategy,
		BaselineOutputDir:  baseline.OutputDir,
		CandidateOutputDir: candidate.OutputDir,
		Metrics: CompareMetrics{
			SuccessAt1:         compareMetric(baseline.Summary.SuccessAt1, candidate.Summary.SuccessAt1, true),
			FinalSuccessRate:   compareMetric(baseline.Summary.FinalSuccessRate, candidate.Summary.FinalSuccessRate, true),
			InputTokens:        compareMetric(floatPointer(float64(baseline.Summary.InputTokens)), floatPointer(float64(candidate.Summary.InputTokens)), false),
			OutputTokens:       compareMetric(floatPointer(float64(baseline.Summary.OutputTokens)), floatPointer(float64(candidate.Summary.OutputTokens)), false),
			TotalTokens:        compareMetric(floatPointer(float64(baseline.Summary.InputTokens+baseline.Summary.OutputTokens)), floatPointer(float64(candidate.Summary.InputTokens+candidate.Summary.OutputTokens)), false),
			TotalAttempts:      compareMetric(floatPointer(float64(baseline.Summary.Attempts)), floatPointer(float64(candidate.Summary.Attempts)), false),
			TotalDurationMS:    compareMetric(floatPointer(float64(baseline.Summary.DurationMS)), floatPointer(float64(candidate.Summary.DurationMS)), false),
			AverageAttempts:    compareMetric(baseline.Summary.AverageAttemptsToValid, candidate.Summary.AverageAttemptsToValid, false),
			AverageScore:       compareMetric(baseline.Summary.AverageScore, candidate.Summary.AverageScore, false),
			CostPerValid:       compareMetric(baseline.Summary.CostPerValid, candidate.Summary.CostPerValid, false),
			AverageTimeToValid: compareMetric(baseline.Summary.AverageTimeToValidMS, candidate.Summary.AverageTimeToValidMS, false),
		},
	}
	if baseline.Mode == "flow" {
		baseFlow, candidateFlow := baseline.Summary.Flow, candidate.Summary.Flow
		if baseFlow == nil {
			baseFlow = &FlowSummary{}
		}
		if candidateFlow == nil {
			candidateFlow = &FlowSummary{}
		}
		out.Metrics.Flow = &FlowCompareMetrics{
			ValidRate:           compareMetric(baseFlow.ValidRate, candidateFlow.ValidRate, true),
			FalseAcceptRate:     compareMetric(baseFlow.FalseAcceptRate, candidateFlow.FalseAcceptRate, true),
			FalseRejectRate:     compareMetric(baseFlow.FalseRejectRate, candidateFlow.FalseRejectRate, true),
			FlowCompletionRate:  compareMetric(baseFlow.FlowCompletionRate, candidateFlow.FlowCompletionRate, true),
			ValidationErrorRate: compareMetric(baseFlow.ValidationErrorRate, candidateFlow.ValidationErrorRate, true),
		}
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
			BaselineOutcome: outcomePointer(base.Outcome), CandidateOutcome: outcomePointer(cand.Outcome),
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

func outcomePointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (r CompareReport) String() string {
	base, candidate := compareOutcomeCounts(r.Cases, true), compareOutcomeCounts(r.Cases, false)
	correctness := assessCounts(base.valid, candidate.valid, true)
	reliability := "SAME"
	if r.Metrics.Flow != nil {
		reliability = combineAssessments(
			assessCounts(base.completed, candidate.completed, true),
			assessCounts(base.falseAccept, candidate.falseAccept, false),
			assessCounts(base.falseReject, candidate.falseReject, false),
			assessCounts(base.validationErrors, candidate.validationErrors, false),
			assessCounts(base.infrastructureErrors, candidate.infrastructureErrors, false),
		)
	}
	efficiency := combineAssessments(
		assessMetric(r.Metrics.TotalTokens, false),
		assessMetric(r.Metrics.TotalDurationMS, false),
	)
	overall := correctness
	if overall == "SAME" {
		overall = reliability
	}
	if overall == "SAME" {
		overall = efficiency
	}

	var output strings.Builder
	table := tabwriter.NewWriter(&output, 0, 2, 2, ' ', 0)
	fmt.Fprintln(table, "COMPARISON")
	fmt.Fprintf(table, "  Benchmark\t%s\t%s\n", valueOrDash(r.Benchmark.ID), shortFingerprint(r.Benchmark.Fingerprint))
	fmt.Fprintf(table, "  A\t%s\n", compareRunLabel(r.BaselineOutputDir, r.Baseline.ID))
	fmt.Fprintf(table, "  B\t%s\n", compareRunLabel(r.CandidateOutputDir, r.Candidate.ID))
	fmt.Fprintf(table, "  Preset A\t%s\n", valueOrDash(r.Baseline.ModelPreset))
	fmt.Fprintf(table, "  Preset B\t%s\n", valueOrDash(r.Candidate.ModelPreset))
	fmt.Fprintln(table, "  Assessment\tB compared with A")

	fmt.Fprintln(table, "\nSUMMARY")
	fmt.Fprintf(table, "  Overall\t%s\n", overall)
	fmt.Fprintf(table, "  Correctness\t%s\n", correctness)
	fmt.Fprintf(table, "  Reliability\t%s\n", reliability)
	fmt.Fprintf(table, "  Efficiency\t%s\n", efficiency)
	fmt.Fprintf(table, "  Evidence\t%d paired runs\n", len(r.Cases))

	fmt.Fprintln(table, "\nCORRECTNESS")
	fmt.Fprintln(table, "  Metric\tA\tB\tChange\tAssessment")
	writeCountComparison(table, "Valid products", base.valid, candidate.valid, len(r.Cases), true)
	if r.Metrics.Flow != nil {
		writeCountComparison(table, "Flow completed", base.completed, candidate.completed, len(r.Cases), true)
		writeCountComparison(table, "False accepts", base.falseAccept, candidate.falseAccept, len(r.Cases), false)
		writeCountComparison(table, "False rejects", base.falseReject, candidate.falseReject, len(r.Cases), false)
		writeCountComparison(table, "Validator errors", base.validationErrors, candidate.validationErrors, len(r.Cases), false)
		writeCountComparison(table, "Infrastructure", base.infrastructureErrors, candidate.infrastructureErrors, len(r.Cases), false)
	}
	if r.Metrics.SuccessAt1.Baseline != nil || r.Metrics.SuccessAt1.Candidate != nil {
		writePercentComparison(table, "Success at 1", r.Metrics.SuccessAt1, true)
	}
	if r.Metrics.AverageScore.Baseline != nil || r.Metrics.AverageScore.Candidate != nil {
		writeNumberComparison(table, "Average score", r.Metrics.AverageScore, true, base.valid, candidate.valid, false)
	}

	fmt.Fprintln(table, "\nEFFICIENCY")
	fmt.Fprintln(table, "  Metric\tA\tB\tChange\tAssessment")
	writeWholeComparison(table, "Total tokens", r.Metrics.TotalTokens, false)
	writeDurationComparison(table, "Duration", r.Metrics.TotalDurationMS, false, base.valid, candidate.valid, false)
	writeWholeComparison(table, "Node attempts", r.Metrics.TotalAttempts, false)
	writeNumberComparison(table, "Attempts per valid", r.Metrics.AverageAttempts, false, base.valid, candidate.valid, true)
	writeNumberComparison(table, "Cost per valid", r.Metrics.CostPerValid, false, base.valid, candidate.valid, true)
	writeDurationComparison(table, "Time to valid", r.Metrics.AverageTimeToValid, false, base.valid, candidate.valid, true)

	fmt.Fprintln(table, "\nMODELS")
	fmt.Fprintln(table, "  Alias\tA\tB")
	aliases := compareModelAliases(r.Baseline.Models, r.Candidate.Models)
	for _, alias := range aliases {
		fmt.Fprintf(table, "  %s\t%s\t%s\n", alias, valueOrDash(r.Baseline.Models[alias]), valueOrDash(r.Candidate.Models[alias]))
	}
	if len(aliases) == 0 {
		fmt.Fprintln(table, "  -\t-\t-")
	}

	fmt.Fprintln(table, "\nCASES")
	fmt.Fprintln(table, "  Case\tA\tB\tAssessment")
	for _, item := range r.Cases {
		fmt.Fprintf(table, "  %s#%d\t%s\t%s\t%s\n", item.CaseID, item.Repeat, compareCaseResult(item.BaselineOutcome, item.BaselineValid), compareCaseResult(item.CandidateOutcome, item.CandidateValid), caseAssessment(item.Transition))
	}
	if len(r.Cases) == 0 {
		fmt.Fprintln(table, "  -\t-\t-\t-")
	}
	_ = table.Flush()
	return strings.TrimSpace(output.String())
}

type compareCounts struct {
	valid, completed, falseAccept, falseReject, validationErrors, infrastructureErrors int
}

func compareOutcomeCounts(cases []CaseComparison, baseline bool) compareCounts {
	var counts compareCounts
	for _, item := range cases {
		valid, outcome := item.CandidateValid, item.CandidateOutcome
		if baseline {
			valid, outcome = item.BaselineValid, item.BaselineOutcome
		}
		if valid {
			counts.valid++
		}
		if outcome == nil {
			counts.validationErrors++
			continue
		}
		switch *outcome {
		case "true_accept":
			counts.completed++
		case "false_accept":
			counts.completed++
			counts.falseAccept++
		case "false_reject":
			counts.falseReject++
		case "infrastructure_error":
			counts.infrastructureErrors++
		}
	}
	return counts
}

func assessCounts(baseline, candidate int, higherBetter bool) string {
	return assessValues(float64(baseline), float64(candidate), higherBetter)
}

func assessMetric(metric MetricComparison, higherBetter bool) string {
	if metric.Baseline == nil && metric.Candidate == nil {
		return "NOT MEASURED"
	}
	if metric.Baseline == nil || metric.Candidate == nil {
		return "NOT COMPARABLE"
	}
	return assessValues(*metric.Baseline, *metric.Candidate, higherBetter)
}

func assessValues(baseline, candidate float64, higherBetter bool) string {
	if baseline == candidate {
		return "SAME"
	}
	better := candidate > baseline
	if !higherBetter {
		better = candidate < baseline
	}
	if better {
		return "BETTER"
	}
	return "WORSE"
}

func assessPerValid(metric MetricComparison, higherBetter bool, baselineValid, candidateValid int) string {
	if metric.Baseline == nil && metric.Candidate == nil {
		return "NOT MEASURED"
	}
	if metric.Baseline == nil {
		if baselineValid == 0 && candidateValid > 0 {
			return "BETTER"
		}
		return "NOT COMPARABLE"
	}
	if metric.Candidate == nil {
		if baselineValid > 0 && candidateValid == 0 {
			return "WORSE"
		}
		return "NOT COMPARABLE"
	}
	return assessValues(*metric.Baseline, *metric.Candidate, higherBetter)
}

func combineAssessments(values ...string) string {
	better, worse, measured := false, false, false
	for _, value := range values {
		switch value {
		case "BETTER":
			better, measured = true, true
		case "WORSE":
			worse, measured = true, true
		case "SAME":
			measured = true
		case "NOT COMPARABLE":
			return "NOT COMPARABLE"
		}
	}
	if better && worse {
		return "NOT COMPARABLE"
	}
	if better {
		return "BETTER"
	}
	if worse {
		return "WORSE"
	}
	if measured {
		return "SAME"
	}
	return "NOT MEASURED"
}

func writeCountComparison(table *tabwriter.Writer, label string, baseline, candidate, total int, higherBetter bool) {
	fmt.Fprintf(table, "  %s\t%s\t%s\t%s\t%s\n", label, formatCountRatio(baseline, total), formatCountRatio(candidate, total), formatSignedInt(candidate-baseline), assessCounts(baseline, candidate, higherBetter))
}

func writePercentComparison(table *tabwriter.Writer, label string, metric MetricComparison, higherBetter bool) {
	fmt.Fprintf(table, "  %s\t%s\t%s\t%s\t%s\n", label, formatPercent(metric.Baseline), formatPercent(metric.Candidate), formatSignedPP(metric.DeltaPP), assessMetric(metric, higherBetter))
}

func writeWholeComparison(table *tabwriter.Writer, label string, metric MetricComparison, higherBetter bool) {
	fmt.Fprintf(table, "  %s\t%s\t%s\t%s\t%s\n", label, formatWholeComparisonValue(metric.Baseline), formatWholeComparisonValue(metric.Candidate), formatNumericChange(metric, true), assessMetric(metric, higherBetter))
}

func writeNumberComparison(table *tabwriter.Writer, label string, metric MetricComparison, higherBetter bool, baselineValid, candidateValid int, perValid bool) {
	assessment := assessMetric(metric, higherBetter)
	if perValid {
		assessment = assessPerValid(metric, higherBetter, baselineValid, candidateValid)
	}
	fmt.Fprintf(table, "  %s\t%s\t%s\t%s\t%s\n", label, formatMeasuredValue(metric.Baseline, baselineValid, perValid), formatMeasuredValue(metric.Candidate, candidateValid, perValid), formatNumericChange(metric, false), assessment)
}

func writeDurationComparison(table *tabwriter.Writer, label string, metric MetricComparison, higherBetter bool, baselineValid, candidateValid int, perValid bool) {
	assessment := assessMetric(metric, higherBetter)
	if perValid {
		assessment = assessPerValid(metric, higherBetter, baselineValid, candidateValid)
	}
	fmt.Fprintf(table, "  %s\t%s\t%s\t%s\t%s\n", label, formatMeasuredDuration(metric.Baseline, baselineValid, perValid), formatMeasuredDuration(metric.Candidate, candidateValid, perValid), formatDurationChange(metric), assessment)
}

func formatCountRatio(value, total int) string {
	if total == 0 {
		return "0/0 (-)"
	}
	return fmt.Sprintf("%d/%d (%.0f%%)", value, total, float64(value)*100/float64(total))
}

func formatSignedInt(value int) string {
	if value > 0 {
		return fmt.Sprintf("+%d", value)
	}
	return fmt.Sprintf("%d", value)
}

func formatSignedPP(value *float64) string {
	if value == nil {
		return "no result"
	}
	return fmt.Sprintf("%+.1f pp", *value)
}

func formatWholeComparisonValue(value *float64) string {
	if value == nil {
		return "not measured"
	}
	return formatNumber(int64(math.Round(*value)))
}

func formatMeasuredValue(value *float64, valid int, perValid bool) string {
	if value == nil {
		if perValid && valid == 0 {
			return "no valid result"
		}
		return "not measured"
	}
	return fmt.Sprintf("%g", *value)
}

func formatMeasuredDuration(value *float64, valid int, perValid bool) string {
	if value == nil {
		if perValid && valid == 0 {
			return "no valid result"
		}
		return "not measured"
	}
	return (time.Duration(math.Round(*value)) * time.Millisecond).String()
}

func formatNumericChange(metric MetricComparison, whole bool) string {
	if metric.Delta == nil {
		return "no result"
	}
	value := fmt.Sprintf("%+g", *metric.Delta)
	if whole {
		value = formatSignedNumber(int64(math.Round(*metric.Delta)))
	}
	if metric.DeltaPercent != nil {
		value += fmt.Sprintf(" (%+.1f%%)", *metric.DeltaPercent)
	}
	return value
}

func formatSignedNumber(value int64) string {
	if value > 0 {
		return "+" + formatNumber(value)
	}
	return formatNumber(value)
}

func formatDurationChange(metric MetricComparison) string {
	if metric.Delta == nil {
		return "no result"
	}
	delta := time.Duration(math.Round(*metric.Delta)) * time.Millisecond
	value := delta.String()
	if delta > 0 {
		value = "+" + value
	}
	if metric.DeltaPercent != nil {
		value += fmt.Sprintf(" (%+.1f%%)", *metric.DeltaPercent)
	}
	return value
}

func compareRunLabel(outputDir, strategyID string) string {
	if outputDir != "" {
		return outputDir
	}
	return valueOrDash(strategyID)
}

func compareModelAliases(baseline, candidate map[string]string) []string {
	seen := map[string]bool{}
	for alias := range baseline {
		seen[alias] = true
	}
	for alias := range candidate {
		seen[alias] = true
	}
	aliases := make([]string, 0, len(seen))
	for alias := range seen {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return aliases
}

func compareCaseResult(outcome *string, valid bool) string {
	if outcome != nil {
		return *outcome
	}
	if valid {
		return "valid"
	}
	return "invalid"
}

func caseAssessment(transition string) string {
	switch transition {
	case "candidate_only_valid":
		return "BETTER"
	case "baseline_only_valid":
		return "WORSE"
	default:
		return "SAME"
	}
}
