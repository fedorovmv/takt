package packagedist

import (
	"fmt"
	"strconv"
	"strings"
)

type semver struct{ major, minor, patch int }

func parseVersion(v string) (semver, error) {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	core := strings.SplitN(v, "-", 2)[0]
	p := strings.Split(core, ".")
	if len(p) != 3 {
		return semver{}, fmt.Errorf("version %q must use major.minor.patch", v)
	}
	nums := make([]int, 3)
	for i := range p {
		n, e := strconv.Atoi(p[i])
		if e != nil || n < 0 {
			return semver{}, fmt.Errorf("invalid version %q", v)
		}
		nums[i] = n
	}
	return semver{nums[0], nums[1], nums[2]}, nil
}
func compare(a, b semver) int {
	if a.major != b.major {
		if a.major < b.major {
			return -1
		}
		return 1
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1
		}
		return 1
	}
	if a.patch != b.patch {
		if a.patch < b.patch {
			return -1
		}
		return 1
	}
	return 0
}
func Satisfies(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return true
	}
	v, e := parseVersion(version)
	if e != nil {
		return false
	}
	if strings.HasPrefix(constraint, ">=") {
		c, e := parseVersion(strings.TrimSpace(strings.TrimPrefix(constraint, ">=")))
		return e == nil && compare(v, c) >= 0
	}
	if strings.HasPrefix(constraint, "^") {
		c, e := parseVersion(strings.TrimSpace(strings.TrimPrefix(constraint, "^")))
		if e != nil || compare(v, c) < 0 {
			return false
		}
		upper := semver{major: c.major + 1}
		if c.major == 0 {
			upper = semver{minor: c.minor + 1}
			if c.minor == 0 {
				upper = semver{patch: c.patch + 1}
			}
		}
		return compare(v, upper) < 0
	}
	c, e := parseVersion(constraint)
	return e == nil && compare(v, c) == 0
}
