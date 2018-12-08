package microsub

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestJson(t *testing.T) {
	item := Item{Type: "entry"}
	result, err := json.Marshal(item)
	fmt.Println(string(result), err)
}
