package yamlcodec

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type UnknownFieldError struct {
	Path       string
	Field      string
	Suggestion string
}

func (e *UnknownFieldError) Error() string {
	if e.Suggestion != "" {
		return fmt.Sprintf("unknown field %q at %s; did you mean %q?", e.Field, e.Path, e.Suggestion)
	}
	return fmt.Sprintf("unknown field %q at %s", e.Field, e.Path)
}

func validateKnownFields(value any, target reflect.Type, path string) error {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if target.PkgPath() == "encoding/json" && target.Name() == "RawMessage" {
		return nil
	}
	switch target.Kind() {
	case reflect.Interface:
		return nil
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		fields := jsonFields(target)
		for key, child := range object {
			field, exists := fields[key]
			if !exists {
				return &UnknownFieldError{Path: path, Field: key, Suggestion: nearestField(key, fields)}
			}
			if err := validateKnownFields(child, field.Type, path+"."+key); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		items, ok := value.([]any)
		if !ok {
			return nil
		}
		for index, child := range items {
			if err := validateKnownFields(child, target.Elem(), fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for key, child := range object {
			if err := validateKnownFields(child, target.Elem(), path+"."+key); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonFields(target reflect.Type) map[string]reflect.StructField {
	out := map[string]reflect.StructField{}
	for index := 0; index < target.NumField(); index++ {
		field := target.Field(index)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		out[name] = field
	}
	return out
}

func nearestField(value string, fields map[string]reflect.StructField) string {
	if len(fields) == 0 {
		return ""
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	best, bestDistance := "", 1<<30
	for _, name := range names {
		distance := editDistance(strings.ToLower(value), strings.ToLower(name))
		if distance < bestDistance {
			best, bestDistance = name, distance
		}
	}
	limit := 2
	if len(value) >= 8 {
		limit = 3
	}
	if bestDistance > limit {
		return ""
	}
	return best
}

func editDistance(left, right string) int {
	a, b := []rune(left), []rune(right)
	previous := make([]int, len(b)+1)
	for index := range previous {
		previous[index] = index
	}
	for i, l := range a {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j, r := range b {
			cost := 0
			if l != r {
				cost = 1
			}
			current[j+1] = min3(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
