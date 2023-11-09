/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */
package jf2_test

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"testing"

	"p83.nl/go/ekster/pkg/jf2"
	"p83.nl/go/ekster/pkg/microsub"

	"github.com/stretchr/testify/assert"
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
	jf2.ConvertItem(&item, &mdItem)

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
	jf2.ConvertItem(&item, &mdItem)

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
	jf2.ConvertItem(&item, &mdItem)

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
		items := jf2.SimplifyMicroformatDataItems(&mdItem)
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
		items := jf2.SimplifyMicroformatDataItems(&mdItem)
		assert.Len(t, items, 1)
		item := items[0]
		assert.Equal(t, "Testing NODE RED", item.Name)
		assert.Equal(t, "Hello world", item.Content.Text)
		assert.Equal(t, "Peter Stuifzand", item.Author.Name)
	}
}

func TestCleanHTML(t *testing.T) {
	clean, err := jf2.CleanHTML(`<div style="white-space: pre">test</div>`)
	if assert.NoError(t, err) {
		assert.Equal(t, "<div>test</div>", clean)
	}
}

func TestCleanHTMLSimpler(t *testing.T) {
	clean, err := jf2.CleanHTML(`<div>test</div><div>test2</div>`)
	if assert.NoError(t, err) {
		assert.Equal(t, "<div>test</div><div>test2</div>", clean)
	}
}

func TestConvertItemNoteWithCheckout(t *testing.T) {
	f, err := os.Open("./tests/note-with-checkout.html")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	u, err := url.Parse("https://aaronparecki.com/2020/08/21/16/")
	if err != nil {
		log.Fatal(err)
	}

	data := microformats.Parse(f, u)
	results := jf2.SimplifyMicroformatDataItems(data)
	assert.Len(t, results, 1, "need 1 item")

	assert.NotNil(t, results[0].Content)
	assert.Equal(
		t,
		"not sure if it's cheaper to buy all the Microsoft Flight Simulator accessories or actually train for a pilots license 🤔 https://youtu.be/shpK1Gjvnuo",
		results[0].Content.Text)
	assert.Equal(
		t,
		"not sure if it&#39;s cheaper to buy all the Microsoft Flight Simulator accessories or actually train for a pilots license <a href=\"https://aaronparecki.com/emoji/%F0%9F%A4%94\" class=\"emoji\">🤔</a> <a href=\"https://youtu.be/shpK1Gjvnuo\"><span class=\"protocol\">https://</span>youtu.be/shpK1Gjvnuo</a>",
		results[0].Content.HTML)
}
