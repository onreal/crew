package sqlite

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const sqliteTimestampLayout = "2006-01-02T15:04:05.000000000Z"

func marshalJSON(value any) (string, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func mustMarshalJSON(value any) string {
	content, err := marshalJSON(value)
	if err != nil {
		panic(err)
	}

	return content
}

func parseTimestamp(value string) (time.Time, error) {
	ts, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", value, err)
	}

	return ts, nil
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(sqliteTimestampLayout)
}

func newClaimToken() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("read claim token entropy: %w", err)
	}

	return hex.EncodeToString(bytes[:]), nil
}
