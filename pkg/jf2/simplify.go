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
	"log"
	"strings"

	"p83.nl/go/ekster/pkg/microsub"

	"willnorris.com/go/microformats"
)

func simplifyRefItem(k string, v []interface{}) (string, bool, microsub.Item) {
	item := microsub.Item{}

	for _, x := range v {
		switch t := x.(type) {
		case *microformats.Microformat:
			item, ok := SimplifyMicroformatItem(t, microsub.Card{})
			if ok {
				return item.URL, true, item
			}
			return "", false, item
		case string:
			return t, false, item
		default:
			log.Printf("simplifyRefItem(%s, %+v): unsupported type %T", k, v, t)
		}
	}

	return "", false, item
}

func simplifyContent(k string, v []interface{}) *microsub.Content {
	if len(v) == 0 {
		return nil
	}

	var content microsub.Content
	switch t := v[0].(type) {
	case map[string]interface{}:
		if text, e := t["value"]; e {
			content.Text = text.(string)
		}
		if text, e := t["html"]; e {
			content.HTML = text.(string)
		}
	default:
		log.Printf("simplifyContent(%s, %+v): unsupported type %T", k, v, t)
		return nil
	}
	return &content
}

func itemPtr(item *microsub.Item, key string) *[]string {
	if key == "bookmark-of" {
		return &item.BookmarkOf
	} else if key == "repost-of" {
		return &item.RepostOf
	} else if key == "like-of" {
		return &item.LikeOf
	} else if key == "in-reply-to" {
		return &item.InReplyTo
	} else if key == "photo" {
		return &item.Photo
	} else if key == "category" {
		return &item.Category
	}
	return nil
}

func simplifyToItem(itemType string, item map[string][]interface{}) microsub.Item {
	var feedItem microsub.Item

	if itemType == "cite" {
		itemType = "entry"
	}
	feedItem.Type = itemType
	feedItem.Refs = make(map[string]microsub.Item)

	for k, v := range item {
		switch k {
		case "bookmark-of", "like-of", "repost-of", "in-reply-to":
			u, withItem, refItem := simplifyRefItem(k, v)

			if resultPtr := itemPtr(&feedItem, k); resultPtr != nil {
				*resultPtr = append(*resultPtr, u)
				if withItem {
					feedItem.Refs[u] = refItem
				}
			}
		case "content":
			content := simplifyContent(k, v)
			feedItem.Content = content
		case "author":
			author, _ := simplifyCard(v[0])
			feedItem.Author = &author
		case "checkin":
			author, _ := simplifyCard(v[0])
			feedItem.Checkin = &author
		case "name", "published", "updated", "url", "uid", "latitude", "longitude":
			if resultPtr := getScalarPtr(&feedItem, k); resultPtr != nil {
				if len(v) >= 1 {
					*resultPtr = v[0].(string)
				}
			}
		case "category":
			if resultPtr := itemPtr(&feedItem, k); resultPtr != nil {
				for _, c := range v {
					switch t := c.(type) {
					case microformats.Microformat:
						// TODO: perhaps use name
						if t.Value != "" {
							*resultPtr = append(*resultPtr, t.Value)
						}
					case string:
						*resultPtr = append(*resultPtr, t)
					}
				}
			}
		default:
			log.Printf("simplifyToItem: not supported: %s => %v\n", k, v)
		}
	}

	// Remove "name" when it's equals to "content[text]"
	if feedItem.Content != nil {
		if strings.TrimSpace(feedItem.Name) == strings.TrimSpace(feedItem.Content.Text) {
			feedItem.Name = ""
		}
	}

	return feedItem
}

func getScalarPtr(item *microsub.Item, k string) *string {
	switch k {
	case "published":
		return &item.Published
	case "updated":
		return &item.Updated
	case "name":
		return &item.Name
	case "uid":
		return &item.UID
	case "url":
		return &item.URL
	case "latitude":
		return &item.Latitude
	case "longitude":
		return &item.Longitude
	}
	return nil
}

func simplifyCard(v interface{}) (microsub.Card, bool) {
	author := microsub.Card{}
	author.Type = "card"

	switch t := v.(type) {
	case *microformats.Microformat:
		return simplifyCardFromMicroformat(author, t)
	case string:
		return simplifyCardFromString(author, t)
	}

	return author, false
}

func simplifyCardFromString(card microsub.Card, value string) (microsub.Card, bool) {
	card.URL = value
	return card, false
}

func simplifyCardFromMicroformat(card microsub.Card, microformat *microformats.Microformat) (microsub.Card, bool) {
	for ik, vk := range microformat.Properties {
		if p, ok := vk[0].(string); ok {
			switch ik {
			case "name":
				card.Name = p
			case "url":
				card.URL = p
			case "photo":
				card.Photo = p
			case "locality":
				card.Locality = p
			case "region":
				card.Region = p
			case "country-name":
				card.CountryName = p
			case "longitude":
				card.Longitude = p
			case "latitude":
				card.Latitude = p
			default:
				log.Printf("In simplifyCard: unknown property %q with value %q\n", ik, p)
			}
		}
	}

	return card, true
}

func SimplifyMicroformatItem(mdItem *microformats.Microformat, author microsub.Card) (microsub.Item, bool) {
	item := microsub.Item{}

	itemType := mdItem.Type[0][2:]
	if itemType != "entry" && itemType != "event" && itemType != "cite" {
		return item, false
	}

	return simplifyToItem(itemType, mdItem.Properties), true
}

func hasType(item *microformats.Microformat, itemType string) bool {
	return len(item.Type) >= 1 && item.Type[0] == itemType
}

func SimplifyMicroformatDataItems(md *microformats.Data) []microsub.Item {
	var items []microsub.Item

	for _, item := range md.Items {
		if hasType(item, "h-feed") {
			var feedAuthor microsub.Card

			if author, e := item.Properties["author"]; e && len(author) > 0 {
				feedAuthor, _ = simplifyCard(author[0])
			}

			for _, childItem := range item.Children {
				if newItem, ok := SimplifyMicroformatItem(childItem, feedAuthor); ok {
					items = append(items, newItem)
				}
			}

			return items
		}

		if newItem, ok := SimplifyMicroformatItem(item, microsub.Card{}); ok {
			items = append(items, newItem)
		}
	}
	return items
}

func SimplifyMicroformatDataAuthor(md *microformats.Data) (microsub.Card, bool) {
	card := microsub.Card{}

	for _, item := range md.Items {
		if hasType(item, "h-card") {
			return simplifyCard(item)
		}
	}

	return card, false
}
