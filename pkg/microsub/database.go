package microsub

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// Scan helps to scan json data from database
func (item *Item) Scan(value interface{}) error {
	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(b, &item)
}

// Value helps to add json data to database
func (item *Item) Value() (driver.Value, error) {
	return json.Marshal(item)
}
