package jsonex

import (
	"encoding/json"
	"errors"
	"io"
)

func DeserializeJson(body io.ReadCloser, result any) error {
	if body == nil {
		return errors.New("body must not be nil")
	}

	if result == nil {
		return errors.New("result must not be nil")
	}

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
