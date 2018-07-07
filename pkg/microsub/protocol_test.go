package microsub

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/pstuifzand/ekster/microsub"
)

func TestJson(t *testing.T) {
	item := microsub.Item{Type: "entry"}
	result, err := json.Marshal(item)
	fmt.Println(string(result), err)
}
