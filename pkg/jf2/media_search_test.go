package jf2

import (
	"reflect"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

func TestSimplifyMediaKinds(t *testing.T) {
	item := simplifyToItem("entry", map[string][]interface{}{"photo": {"p"}, "video": {"v"}, "audio": {"a"}}, microsub.Card{})
	if !reflect.DeepEqual(item.Photo, []string{"p"}) || !reflect.DeepEqual(item.Video, []string{"v"}) || !reflect.DeepEqual(item.Audio, []string{"a"}) {
		t.Fatalf("media: %+v", item)
	}
}
