package main

import (
	"fmt"
	"os"

	"github.com/blevesearch/bleve/v2"
	"github.com/davecgh/go-spew/spew"
	"p83.nl/go/ekster/pkg/microsub"
)

var index bleve.Index

func initSearch() error {
	if _, err := os.Stat("items.bleve"); os.IsNotExist(err) {
		mapping := bleve.NewIndexMapping()
		index, err = bleve.New("items.bleve", mapping)
		if err != nil {
			return err
		}
	} else {
		index, err = bleve.Open("items.bleve")
		if err != nil {
			return fmt.Errorf("while opening search index: %v", err)
		}
		return nil
	}
	return nil
}

func addToSearch(item microsub.Item) error {
	// TODO: add channel when indexing
	if index != nil {
		err := index.Index(item.ID, item)
		if err != nil {
			return fmt.Errorf("while indexing item: %v", err)
		}
	}
	return nil
}

func getString(fields map[string]interface{}, key, def string) string {
	if value, e := fields[key]; e {
		if str, ok := value.(string); ok {
			return str
		}
	}

	return def
}

func querySearch(channel, query string) ([]microsub.Item, error) {
	q := bleve.NewQueryStringQuery(query)

	cq := bleve.NewConjunctionQuery(q)

	if channel != "global" {
		mq := bleve.NewMatchQuery(channel)
		mq.SetField("channel")
		cq.AddQuery(mq)
	}

	req := bleve.NewSearchRequest(cq)
	req.Fields = []string{"*"}
	res, err := index.Search(req)
	if err != nil {
		return nil, fmt.Errorf("while query %q: %v", query, err)
	}

	items := []microsub.Item{}

	hits := res.Hits
	for _, hit := range hits {
		fields := hit.Fields
		var item microsub.Item
		spew.Dump(fields)
		item.Type = getString(fields, "type", "entry")
		item.Name = getString(fields, "name", "")
		item.Content = &microsub.Content{}
		item.Content.HTML = getString(fields, "content.html", "")
		item.Content.Text = getString(fields, "content.text", "")
		item.URL = getString(fields, "url", "")
		item.Name = getString(fields, "name", "")
		items = append(items, item)
	}

	return items, nil
}
