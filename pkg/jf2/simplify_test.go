/*
   ekster - microsub server
   Copyright (C) 2018  Peter Stuifzand

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package jf2

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"p83.nl/go/ekster/pkg/microsub"
	"willnorris.com/go/microformats"
)

// func TestInReplyTo(t *testing.T) {
//
// 	f, err := os.Open("./tests/tantek-in-reply-to.html")
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer f.Close()
//
// 	u, err := url.Parse("http://tantek.com/2018/115/t1/")
// 	if err != nil {
// 		log.Fatal(err)
// 	}
//
// 	data := microformats.Parse(f, u)
// 	results := SimplifyMicroformatData(data)
//
// 	if results[0]["type"] != "entry" {
// 		t.Fatalf("not an h-entry, but %s", results[0]["type"])
// 	}
// 	if results[0]["in-reply-to"] != "https://github.com/w3c/csswg-drafts/issues/2589" {
// 		t.Fatalf("not in-reply-to, but %s", results[0]["in-reply-to"])
// 	}
// 	if results[0]["syndication"] != "https://github.com/w3c/csswg-drafts/issues/2589#thumbs_up-by-tantek" {
// 		t.Fatalf("not in-reply-to, but %s", results[0]["syndication"])
// 	}
// 	if results[0]["published"] != "2018-04-25 11:14-0700" {
// 		t.Fatalf("not published, but %s", results[0]["published"])
// 	}
// 	if results[0]["updated"] != "2018-04-25 11:14-0700" {
// 		t.Fatalf("not updated, but %s", results[0]["updated"])
// 	}
// 	if results[0]["url"] != "http://tantek.com/2018/115/t1/" {
// 		t.Fatalf("not url, but %s", results[0]["url"])
// 	}
// 	if results[0]["uid"] != "http://tantek.com/2018/115/t1/" {
// 		t.Fatalf("not uid, but %s", results[0]["url"])
// 	}
//
// 	if authorValue, e := results[0]["author"]; e {
// 		if author, ok := authorValue.(map[string]string); ok {
// 			if author["name"] != "Tantek Çelik" {
// 				t.Fatalf("name is not expected name, but %q", author["name"])
// 			}
// 			if author["photo"] != "http://tantek.com/logo.jpg" {
// 				t.Fatalf("photo is not expected photo, but %q", author["photo"])
// 			}
// 			if author["url"] != "http://tantek.com/" {
// 				t.Fatalf("url is not expected url, but %q", author["url"])
// 			}
// 		} else {
// 			t.Fatal("author not a map")
// 		}
// 	} else {
// 		t.Fatal("author missing")
// 	}
//
// 	if contentValue, e := results[0]["content"]; e {
// 		if content, ok := contentValue.(map[string]string); ok {
// 			if content["text"] != "👍" {
// 				t.Fatal("text content missing")
// 			}
// 			if content["html"] != "👍" {
// 				t.Fatal("html content missing")
// 			}
// 		}
// 	}
// }

func TestConvertItem0(t *testing.T) {
	var item microsub.Item
	var mdItem microformats.Microformat
	f, err := os.Open("tests/test0.json")
	if err != nil {
		t.Fatalf("error while opening test0.json: %s", err)
	}
	json.NewDecoder(f).Decode(&mdItem)
	ConvertItem(&item, &mdItem)

	if item.Type != "entry" {
		t.Errorf("Expected Type entry, was %q", item.Type)
	}
	if item.Name != "name test" {
		t.Errorf("Expected Name == %q, was %q", "name test", item.Name)
	}
}

func TestConvertItem1(t *testing.T) {
	var item microsub.Item
	var mdItem microformats.Microformat
	f, err := os.Open("tests/test1.json")
	if err != nil {
		t.Fatalf("error while opening test1.json: %s", err)
	}
	json.NewDecoder(f).Decode(&mdItem)
	ConvertItem(&item, &mdItem)

	if item.Type != "entry" {
		t.Errorf("Expected Type entry, was %q", item.Type)
	}
	if item.Author.Type != "card" {
		t.Errorf("Expected Author.Type card, was %q", item.Author.Type)
	}
	if item.Author.Name != "Peter" {
		t.Errorf("Expected Author.Name == %q, was %q", "Peter", item.Author.Name)
	}
}

func TestConvertItem2(t *testing.T) {
	var item microsub.Item
	var mdItem microformats.Microformat
	f, err := os.Open("tests/test2.json")
	if err != nil {
		t.Fatalf("error while opening test2.json: %s", err)
	}
	json.NewDecoder(f).Decode(&mdItem)
	ConvertItem(&item, &mdItem)

	if item.Type != "entry" {
		t.Errorf("Expected Type entry, was %q", item.Type)
	}
	if item.Photo[0] != "https://peterstuifzand.nl/img/profile.jpg" {
		t.Errorf("Expected Photo[0], was %q", item.Type)
	}
	if item.Author.Type != "card" {
		t.Errorf("Expected Author.Type card, was %q", item.Author.Type)
	}
	if item.Author.Name != "Peter" {
		t.Errorf("Expected Author.Name == %q, was %q", "Peter", item.Author.Name)
	}
}

func TestConvert992(t *testing.T) {
	var mdItem microformats.Data
	f, err := os.Open("tests/992.json")
	if err != nil {
		t.Fatalf("error while opening 992.json: %s", err)
	}
	err = json.NewDecoder(f).Decode(&mdItem)
	if assert.NoError(t, err) {
		items := SimplifyMicroformatDataItems(&mdItem)
		assert.Len(t, items, 1)
		item := items[0]
		assert.Equal(t, "https://p83.nl/posts/992", item.URL)
		assert.Equal(t, "https://p83.nl/posts/992", item.UID)
		assert.Equal(t, "2018-12-09T14:14:13Z", item.Published)
		assert.Equal(t, "https://twitter.com/InDeepGeek/status/1071363145485168640", item.LikeOf[0])
		assert.Equal(t, "entry", item.Type)
		assert.Equal(t, "", item.Name)
		assert.Equal(t, "test", item.Content.Text)
		assert.Equal(t, "<p>test</p>", item.Content.HTML)

		author := item.Author
		assert.Equal(t, "card", author.Type)
	}
}

func TestConvertAuthor(t *testing.T) {
	var mdItem microformats.Data
	f, err := os.Open("tests/author.json")
	if err != nil {
		t.Fatalf("error while opening author.json: %s", err)
	}
	err = json.NewDecoder(f).Decode(&mdItem)
	if assert.NoError(t, err) {
		items := SimplifyMicroformatDataItems(&mdItem)
		assert.Len(t, items, 1)
		item := items[0]
		assert.Equal(t, "Testing NODE RED", item.Name)
		assert.Equal(t, "Hello world", item.Content.Text)
		assert.Equal(t, "Peter Stuifzand", item.Author.Name)
	}
}
