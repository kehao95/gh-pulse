package assertion

import (
	"fmt"
	"strings"
)

type Assertion struct {
	Path     string
	Operator string
	Value    string
	ExitCode int
}

func ParseAssertion(input string, exitCode int) (Assertion, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Assertion{}, fmt.Errorf("assertion cannot be empty")
	}

	if strings.Contains(trimmed, "=") {
		idx := strings.IndexRune(trimmed, '=')
		if idx == 0 {
			return Assertion{}, fmt.Errorf("missing path before '='")
		}
		path := strings.TrimSpace(trimmed[:idx])
		value := strings.TrimSpace(trimmed[idx+1:])
		if path == "" {
			return Assertion{}, fmt.Errorf("missing path before '='")
		}
		if value == "" {
			return Assertion{}, fmt.Errorf("missing value after '='")
		}
		if strings.HasPrefix(value, "~") {
			pattern := strings.TrimSpace(value[1:])
			if pattern == "" {
				return Assertion{}, fmt.Errorf("missing regex pattern after '=~'")
			}
			return Assertion{
				Path:     path,
				Operator: "regex",
				Value:    pattern,
				ExitCode: exitCode,
			}, nil
		}
		return Assertion{
			Path:     path,
			Operator: "eq",
			Value:    value,
			ExitCode: exitCode,
		}, nil
	}

	fields := strings.Fields(trimmed)
	if len(fields) != 2 || fields[1] != "exists" {
		return Assertion{}, fmt.Errorf("expected 'path=value', 'path=~regex', or 'path exists'")
	}
	if fields[0] == "" {
		return Assertion{}, fmt.Errorf("missing path before 'exists'")
	}
	return Assertion{
		Path:     fields[0],
		Operator: "exists",
		Value:    "",
		ExitCode: exitCode,
	}, nil
}

func ParseAssertions(inputs []string, exitCode int) ([]Assertion, error) {
	assertions := make([]Assertion, 0, len(inputs))
	for _, input := range inputs {
		assertion, err := ParseAssertion(input, exitCode)
		if err != nil {
			return nil, fmt.Errorf("invalid assertion %q: %w", input, err)
		}
		assertions = append(assertions, assertion)
	}
	return assertions, nil
}
