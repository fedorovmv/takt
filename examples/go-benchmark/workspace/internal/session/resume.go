package session

import "fmt"

func Resolve(requested string, observed []string) (string, bool, error) {
	if len(observed) == 0 || observed[0] == "" {
		return "", false, fmt.Errorf("session stream did not expose an ID")
	}
	id := observed[0]
	for _, candidate := range observed[1:] {
		if candidate != id {
			return "", false, fmt.Errorf("session changed from %q to %q", id, candidate)
		}
	}
	return id, requested != "", nil
}
