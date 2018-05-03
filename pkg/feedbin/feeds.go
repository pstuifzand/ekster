package feedbin

import (
	"encoding/json"
	"fmt"
)

// Feedbin is the main object for calling the Feedbin API
type Feedbin struct {
	user     string
	password string
}

// New returns a new Feedbin object with the provided user and password
func New(user, password string) *Feedbin {
	var fb Feedbin
	fb.user = user
	fb.password = password
	return &fb
}

// Taggings returns taggings
func (fb *Feedbin) Taggings() ([]Tagging, error) {
	resp, err := fb.get("/v2/taggings.json")
	if err != nil {
		return []Tagging{}, err
	}
	defer resp.Body.Close()

	var taggings []Tagging

	dec := json.NewDecoder(resp.Body)
	dec.Decode(&taggings)

	return taggings, nil
}

// Feed returns a Feed for id
func (fb *Feedbin) Feed(id int64) (Feed, error) {
	resp, err := fb.get(fmt.Sprintf("/v2/feeds/%d.json", id))
	if err != nil {
		return Feed{}, err
	}

	defer resp.Body.Close()

	var feed Feed

	dec := json.NewDecoder(resp.Body)
	dec.Decode(&feed)

	return feed, nil
}

// Entries return a slice of entries
func (fb *Feedbin) Entries() ([]Entry, error) {
	resp, err := fb.get("/v2/entries.json")
	if err != nil {
		return []Entry{}, err
	}
	defer resp.Body.Close()

	var entries []Entry

	dec := json.NewDecoder(resp.Body)
	dec.Decode(&entries)

	return entries, nil
}
