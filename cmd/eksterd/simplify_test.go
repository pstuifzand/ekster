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
}
