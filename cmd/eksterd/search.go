package main

import (
	"fmt"
	"os"

	"github.com/blevesearch/bleve/v2"
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

type indexItem struct {
	microsub.Item
	Channel string `json:"channel"`
}

func addToSearch(item microsub.Item, channel string) error {
	// TODO: add channel when indexing
	if index != nil {
		indexItem := indexItem{item, channel}
		err := index.Index(item.ID, indexItem)
		if err != nil {
			return fmt.Errorf("while indexing item: %v", err)
		}
	}
	return nil
}

func getStringArray(fields map[string]interface{}, key string) []string {
	if value, e := fields[key]; e {
		if str, ok := value.([]string); ok {
			return str
		}
	}
	return []string{}
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

	/*
	   web_1       |  (string) (len=19) "author.country-name": (string) "",
	   web_1       |  (string) (len=15) "author.latitude": (string) "",
	   web_1       |  (string) (len=13) "author.region": (string) ""
	   web_1       |  (string) (len=15) "author.locality": (string) "",
	   web_1       |  (string) (len=16) "author.longitude": (string) "",
	*/

	hits := res.Hits
	for _, hit := range hits {
		fields := hit.Fields
		var item microsub.Item
		item.UID = getString(fields, "uid", "")
		item.Type = getString(fields, "type", "entry")
		item.Name = getString(fields, "name", "")
		item.Content = &microsub.Content{}
		item.Content.HTML = getString(fields, "content.html", "")
		item.Content.Text = getString(fields, "content.text", "")
		item.Summary = getString(fields, "summary", "")
		item.URL = getString(fields, "url", "")
		item.Name = getString(fields, "name", "")
		item.Longitude = getString(fields, "longitude", "")
		item.Latitude = getString(fields, "latitude", "")
		item.Published = getString(fields, "published", "")
		item.Updated = getString(fields, "updated", "")
		item.Read = false
		item.Author = &microsub.Card{
			Type:  getString(fields, "author.type", ""),
			Name:  getString(fields, "author.name", ""),
			URL:   getString(fields, "author.url", ""),
			Photo: getString(fields, "author.photo", ""),
		}
		item.Category = getStringArray(fields, "category")
		items = append(items, item)
	}

	return items, nil
}
