package execution

import "strings"

var transientProviderPhrases = []string{
	"rate limit", "rate_limit", "too many requests", "overloaded",
	"service unavailable", "temporarily unavailable", "temporary unavailable",
	"connection reset", "connection refused", "econnreset", "etimedout",
	"enotfound", "no such host", "temporary failure in name resolution",
	"fetch failed", "socket hang up",
}

func ProviderUnavailable(err error) bool {
	return KindOf(err) == KindProviderUnavailable
}

func IsTransientProviderFailure(status int, message string) bool {
	switch status {
	case 429, 502, 503, 504:
		return true
	}
	message = strings.ToLower(message)
	for _, phrase := range transientProviderPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
