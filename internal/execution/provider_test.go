package execution

import "testing"

func TestTransientProviderFailureStatuses(t *testing.T) {
	for _, status := range []int{429, 502, 503, 504} {
		if !IsTransientProviderFailure(status, "") {
			t.Errorf("status %d was not classified as transient", status)
		}
	}
}

func TestTransientProviderFailurePhrases(t *testing.T) {
	phrases := []string{
		"rate limit", "rate_limit", "too many requests", "overloaded",
		"service unavailable", "temporarily unavailable", "temporary unavailable",
		"connection reset", "connection refused", "econnreset", "etimedout",
		"enotfound", "no such host", "temporary failure in name resolution",
		"fetch failed", "socket hang up",
	}
	for _, phrase := range phrases {
		if !IsTransientProviderFailure(0, phrase) {
			t.Errorf("phrase %q was not classified as transient", phrase)
		}
	}
	if !IsTransientProviderFailure(0, "Provider RATE LIMIT exceeded") {
		t.Fatal("phrase matching should normalize case")
	}
}

func TestTransientProviderFailureNegativeCases(t *testing.T) {
	for _, status := range []int{400, 401, 404} {
		if IsTransientProviderFailure(status, "") {
			t.Errorf("status %d was incorrectly classified as transient", status)
		}
	}
	for _, message := range []string{"context length", "tool failed", "arbitrary assistant prose"} {
		if IsTransientProviderFailure(0, message) {
			t.Errorf("message %q was incorrectly classified as transient", message)
		}
	}
}
