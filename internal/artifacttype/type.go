package artifacttype

import "regexp"

const Pattern = `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`

var expression = regexp.MustCompile(Pattern)

func Valid(value string) bool { return expression.MatchString(value) }
