package server

import (
	"encoding/json"
	"sort"
	"strings"
)

const (
	maxPayloadSize   = 512 * 1024
	maxArrayElements = 10
)

var truncatableKeys = []string{
	"commits",
	"files",
	"added",
	"removed",
	"modified",
	"pages",
}

type truncationInfo struct {
	OriginalCount int `json:"original_count"`
	Kept          int `json:"kept"`
}

func truncatePayloadIfNeeded(body []byte) (json.RawMessage, bool, map[string]truncationInfo, error) {
	if len(body) <= maxPayloadSize {
		return json.RawMessage(body), false, nil, nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return json.RawMessage(body), false, nil, err
	}

	truncations := make(map[string]truncationInfo)
	for _, key := range truncatableKeys {
		value, ok := payload[key]
		if !ok {
			continue
		}

		arrayValue, ok := value.([]interface{})
		if !ok {
			continue
		}

		if len(arrayValue) <= maxArrayElements {
			continue
		}

		payload[key] = arrayValue[:maxArrayElements]
		truncations[key] = truncationInfo{OriginalCount: len(arrayValue), Kept: maxArrayElements}
	}

	if len(truncations) == 0 {
		return json.RawMessage(body), false, nil, nil
	}

	payload["_truncated"] = truncations
	encoded, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(body), false, nil, err
	}

	return json.RawMessage(encoded), true, truncations, nil
}

func truncationFields(truncations map[string]truncationInfo) string {
	if len(truncations) == 0 {
		return ""
	}

	keys := make([]string, 0, len(truncations))
	for key := range truncations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
