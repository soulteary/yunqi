package routingeval

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func Decode(raw []byte) (Dataset, error) {
	var d Dataset
	if len(raw) > 4<<20 {
		return d, errors.New("dataset too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return d, errors.New("invalid evaluation schema")
	}
	if decoder.Decode(new(interface{})) != io.EOF {
		return d, errors.New("trailing evaluation data")
	}
	return d, nil
}
