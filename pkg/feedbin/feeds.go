package feedbin

import (
	"encoding/json"
)

type Feedbin struct {
	user     string
	password string
}

func New(user, password string) *Feedbin {
	var fb Feedbin
	fb.user = user
	fb.password = password
	return &fb
}

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
