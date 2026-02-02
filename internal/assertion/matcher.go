package assertion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Match evaluates an assertion against a JSON message
// Returns true if the assertion matches
func (a *Assertion) Match(data []byte) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("assertion is nil")
	}

	var payload interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return false, err
	}

	value, ok := valueAtPath(payload, a.Path)
	switch a.Operator {
	case "exists":
		return ok, nil
	case "eq":
		if !ok {
			return false, nil
		}
		str, ok := stringifyScalar(value)
		if !ok {
			return false, nil
		}
		return str == a.Value, nil
	case "regex":
		if !ok {
			return false, nil
		}
		str, ok := stringifyScalar(value)
		if !ok {
			return false, nil
		}
		re, err := regexp.Compile(a.Value)
		if err != nil {
			return false, err
		}
		return re.MatchString(str), nil
	default:
		return false, fmt.Errorf("unknown operator %q", a.Operator)
	}
}

func valueAtPath(payload interface{}, path string) (interface{}, bool) {
	if path == "" {
		return nil, false
	}

	current := payload
	parts := strings.Split(path, ".")
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		node, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		child, ok := node[part]
		if !ok {
			return nil, false
		}
		current = child
	}

	return current, true
}

func stringifyScalar(value interface{}) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	case nil:
		return "null", true
	default:
		return "", false
	}
}
