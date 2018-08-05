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
package main

import (
	"log"
	"net/url"
	"os"
	"testing"

	"willnorris.com/go/microformats"
)

func TestInReplyTo(t *testing.T) {

	f, err := os.Open("./tests/tantek-in-reply-to.html")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	u, err := url.Parse("http://tantek.com/2018/115/t1/")
	if err != nil {
		log.Fatal(err)
	}

	data := microformats.Parse(f, u)
	results := simplifyMicroformatData(data)

	if results[0]["type"] != "entry" {
		t.Fatalf("not an h-entry, but %s", results[0]["type"])
	}
	if results[0]["in-reply-to"] != "https://github.com/w3c/csswg-drafts/issues/2589" {
		t.Fatalf("not in-reply-to, but %s", results[0]["in-reply-to"])
	}
	if results[0]["syndication"] != "https://github.com/w3c/csswg-drafts/issues/2589#thumbs_up-by-tantek" {
		t.Fatalf("not in-reply-to, but %s", results[0]["syndication"])
	}
	if results[0]["published"] != "2018-04-25 11:14-0700" {
		t.Fatalf("not published, but %s", results[0]["published"])
	}
	if results[0]["updated"] != "2018-04-25 11:14-0700" {
		t.Fatalf("not updated, but %s", results[0]["updated"])
	}
	if results[0]["url"] != "http://tantek.com/2018/115/t1/" {
		t.Fatalf("not url, but %s", results[0]["url"])
	}
	if results[0]["uid"] != "http://tantek.com/2018/115/t1/" {
		t.Fatalf("not uid, but %s", results[0]["url"])
	}

	if authorValue, e := results[0]["author"]; e {
		if author, ok := authorValue.(map[string]string); ok {
			if author["name"] != "Tantek Çelik" {
				t.Fatalf("name is not expected name, but %q", author["name"])
			}
			if author["photo"] != "http://tantek.com/logo.jpg" {
				t.Fatalf("photo is not expected photo, but %q", author["photo"])
			}
			if author["url"] != "http://tantek.com/" {
				t.Fatalf("url is not expected url, but %q", author["url"])
			}
		} else {
			t.Fatal("author not a map")
		}
	} else {
		t.Fatal("author missing")
	}

	if contentValue, e := results[0]["content"]; e {
		if content, ok := contentValue.(map[string]string); ok {
			if content["text"] != "👍" {
				t.Fatal("text content missing")
			}
			if content["html"] != "👍" {
				t.Fatal("html content missing")
			}
		}
	}
}
