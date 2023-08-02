package domain

import (
	"encoding/json"
	"io"
)

type BodyExtractor struct {
}

func (b *BodyExtractor) ExtractApplicationAndNamespace(body io.ReadCloser) (*ApplicationUpdateMessage, error) {
	bodyBytes, err := io.ReadAll(body)

	if err != nil {
		return nil, err
	}

	bodyObject := ApplicationUpdateMessage{}
	err = json.Unmarshal(bodyBytes, &bodyObject)

	if err != nil {
		return nil, err
	}

	return &bodyObject, nil
}
