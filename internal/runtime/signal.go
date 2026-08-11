package runtime

import (
	"regexp"
	"strings"
)

type SignalDiagnostic string

const (
	SignalMissing   SignalDiagnostic = "signal_missing"
	SignalAmbiguous SignalDiagnostic = "signal_ambiguous"
)

type SignalMatch struct {
	MatchedSignal string
	Diagnostic    *SignalDiagnostic
}

var promiseRE = regexp.MustCompile(`<promise>\s*([A-Z][A-Z0-9_-]{0,63})\s*</promise>`)
var runtimeSignalRE = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,63}$`)

// MatchSignal applies the bounded completion contract to full normalized
// output. Fenced Markdown is removed before occurrence counting.
func MatchSignal(output, expected string) SignalMatch {
	clean := stripSignalFences(output)
	validExpected := runtimeSignalRE.MatchString(expected)
	if !validExpected {
		diagnostic := SignalDiagnostic(SignalMissing)
		return SignalMatch{Diagnostic: &diagnostic}
	}
	occurrences := 0
	expectedOccurrences := 0
	for _, match := range promiseRE.FindAllStringSubmatch(clean, -1) {
		value := match[1]
		if !runtimeSignalRE.MatchString(value) {
			continue
		}
		occurrences++
		if value == expected {
			expectedOccurrences++
		}
	}
	lines := strings.Split(clean, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		if line == expected {
			occurrences++
			expectedOccurrences++
		}
		break
	}
	if expectedOccurrences == 0 {
		diagnostic := SignalDiagnostic(SignalMissing)
		if occurrences > 0 {
			diagnostic = SignalAmbiguous
		}
		return SignalMatch{Diagnostic: &diagnostic}
	}
	if occurrences != 1 || expectedOccurrences != 1 {
		diagnostic := SignalDiagnostic(SignalAmbiguous)
		return SignalMatch{Diagnostic: &diagnostic}
	}
	return SignalMatch{MatchedSignal: expected}
}

func stripSignalFences(output string) string {
	lines := strings.Split(output, "\n")
	var out []string
	var fence byte
	fenceLen := 0
	for _, raw := range lines {
		line := strings.TrimLeft(raw, " ")
		leading := len(raw) - len(line)
		if fence == 0 {
			if leading <= 3 && len(line) >= 3 && (line[0] == '`' || line[0] == '~') {
				marker := line[0]
				count := 0
				for count < len(line) && line[count] == marker {
					count++
				}
				if count >= 3 {
					fence, fenceLen = marker, count
					continue
				}
			}
			out = append(out, raw)
			continue
		}
		count := 0
		for count < len(line) && line[count] == fence {
			count++
		}
		if leading <= 3 && count >= fenceLen && strings.TrimSpace(line[count:]) == "" {
			fence, fenceLen = 0, 0
		}
	}
	return strings.Join(out, "\n")
}
