package social

import (
	"encoding/json"
	"strings"
)

type tikTokAPIError struct {
	Code    int
	Message string
	Raw     string
}

func parseTikTokAPIError(raw json.RawMessage, fallback string) tikTokAPIError {
	if len(raw) == 0 || string(raw) == "null" {
		return tikTokAPIError{Code: 0, Message: "", Raw: ""}
	}

	var obj struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return tikTokAPIError{
			Code:    obj.Code,
			Message: obj.Message,
			Raw:     strings.TrimSpace(string(raw)),
		}
	}

	var msg string
	if err := json.Unmarshal(raw, &msg); err == nil {
		return tikTokAPIError{
			Code:    -1,
			Message: msg,
			Raw:     msg,
		}
	}

	return tikTokAPIError{
		Code:    -1,
		Message: fallback,
		Raw:     strings.TrimSpace(string(raw)),
	}
}
