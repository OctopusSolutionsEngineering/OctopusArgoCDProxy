package jsonex

import (
	"encoding/json"
	"io"
)

func DeserializeJson(body io.ReadCloser, result any) error {
	bodyBytes, err := io.ReadAll(body)

	if err != nil {
		return err
	}

	err = json.Unmarshal(bodyBytes, result)

	if err != nil {
		return err
	}

	return nil
}
