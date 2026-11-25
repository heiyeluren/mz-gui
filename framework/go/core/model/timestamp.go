package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

var ErrInvalidTimestamp = errors.New("Framework 时间无效")

type Timestamp string

func NewTimestamp(value time.Time) (Timestamp, error) {
	if value.IsZero() {
		return "", ErrInvalidTimestamp
	}
	return Timestamp(value.UTC().Format(time.RFC3339Nano)), nil
}

func ParseTimestamp(value string) (Timestamp, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", ErrInvalidTimestamp
	}
	return NewTimestamp(parsed)
}

func (timestamp Timestamp) Time() time.Time {
	value, _ := time.Parse(time.RFC3339Nano, string(timestamp))
	return value.UTC()
}

func (timestamp Timestamp) Equal(other Timestamp) bool {
	return timestamp.Time().Equal(other.Time())
}

func (timestamp Timestamp) MarshalJSON() ([]byte, error) {
	if _, err := ParseTimestamp(string(timestamp)); err != nil {
		return nil, ErrInvalidTimestamp
	}
	return json.Marshal(string(timestamp))
}

func (timestamp *Timestamp) UnmarshalJSON(data []byte) error {
	if timestamp == nil || bytes.Equal(data, []byte("null")) {
		return ErrInvalidTimestamp
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return ErrInvalidTimestamp
	}
	parsed, err := ParseTimestamp(value)
	if err != nil {
		return err
	}
	*timestamp = parsed
	return nil
}
