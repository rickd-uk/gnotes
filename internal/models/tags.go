package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Tags stores note labels as a JSON array, separate from Markdown content.
type Tags []string

func (tags Tags) Value() (driver.Value, error) {
	if tags == nil {
		tags = Tags{}
	}
	encoded, err := json.Marshal(tags)
	return string(encoded), err
}

func (tags *Tags) Scan(value any) error {
	switch value := value.(type) {
	case string:
		return json.Unmarshal([]byte(value), tags)
	case []byte:
		return json.Unmarshal(value, tags)
	case nil:
		*tags = Tags{}
		return nil
	default:
		return fmt.Errorf("unsupported tags value %T", value)
	}
}
