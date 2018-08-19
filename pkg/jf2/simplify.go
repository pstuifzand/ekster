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
	"fmt"
	"log"
	"strings"
	"time"

	"p83.nl/go/ekster/pkg/microsub"

	"willnorris.com/go/microformats"
)

func simplify(itemType string, item map[string][]interface{}, author map[string]string) map[string]interface{} {
	feedItem := make(map[string]interface{})

	for k, v := range item {
		if k == "bookmark-of" || k == "like-of" || k == "repost-of" || k == "in-reply-to" {
			if value, ok := v[0].(*microformats.Microformat); ok {
				if value.Type[0] == "h-cite" {
					refs := make(map[string]interface{})
					u := value.Properties["url"][0].(string)
					refs[u] = SimplifyMicroformat(value, nil)
					feedItem["refs"] = refs
					feedItem[k] = u
				} else {
					feedItem[k] = value.Value
				}
			} else {
				feedItem[k] = v
			}
		} else if k == "summary" {
			if content, ok := v[0].(map[string]interface{}); ok {
				if text, e := content["value"]; e {
					feedItem[k] = text
				}
			} else if summary, ok := v[0].(string); ok {
				feedItem[k] = summary
			}
		} else if k == "content" {
			if content, ok := v[0].(map[string]interface{}); ok {
				if text, e := content["value"]; e {
					delete(content, "value")
					content["text"] = text
				}
				feedItem[k] = content
			}
		} else if k == "photo" {
			if itemType == "card" {
				if len(v) >= 1 {
					if value, ok := v[0].(string); ok {
						feedItem[k] = value
					}
				}
			} else {
				feedItem[k] = v
			}
		} else if k == "video" {
			feedItem[k] = v
		} else if k == "featured" {
			feedItem[k] = v
		} else if k == "checkin" || k == "author" {
			if value, ok := v[0].(string); ok {
				feedItem[k] = value
			}
			card, err := simplifyCard(v)
			if err != nil {
				log.Println(err)
				continue
			}
			feedItem[k] = card
		} else if value, ok := v[0].(*microformats.Microformat); ok {
			mType := value.Type[0][2:]
			m := simplify(mType, value.Properties, author)
			m["type"] = mType
			feedItem[k] = m
		} else if value, ok := v[0].(string); ok {
			feedItem[k] = value
		} else if value, ok := v[0].(map[string]interface{}); ok {
			feedItem[k] = value
		} else if value, ok := v[0].([]interface{}); ok {
			feedItem[k] = value
		}
	}

	// Remove "name" when it's equals to "content[text]"
	if name, e := feedItem["name"]; e {
		if content, e2 := feedItem["content"]; e2 {
			if contentMap, ok := content.(map[string]interface{}); ok {
				if text, e3 := contentMap["text"]; e3 {
					if strings.TrimSpace(name.(string)) == strings.TrimSpace(text.(string)) {
						delete(feedItem, "name")
					}
				}
			}
		}
	}

	if _, e := feedItem["author"]; !e {
		feedItem["author"] = author
	}

	return feedItem
}
func simplifyCard(v []interface{}) (map[string]string, error) {
	if value, ok := v[0].(*microformats.Microformat); ok {
		card := make(map[string]string)
		card["type"] = "card"
		for ik, vk := range value.Properties {
			if p, ok := vk[0].(string); ok {
				card[ik] = p
			}
		}
		return card, nil
	}
	return nil, fmt.Errorf("not convertable to a card %q", v)
}

func SimplifyMicroformat(item *microformats.Microformat, author map[string]string) map[string]interface{} {
	itemType := item.Type[0][2:]
	newItem := simplify(itemType, item.Properties, author)
	newItem["type"] = itemType

	children := []map[string]interface{}{}

	if len(item.Children) > 0 {
		for _, c := range item.Children {
			child := SimplifyMicroformat(c, author)
			if c, e := child["children"]; e {
				if ar, ok := c.([]map[string]interface{}); ok {
					children = append(children, ar...)
				}
				delete(child, "children")
			}
			children = append(children, child)
		}

		newItem["children"] = children
	}

	return newItem
}

func SimplifyMicroformatData(md *microformats.Data) []map[string]interface{} {
	var items []map[string]interface{}

	for _, item := range md.Items {
		if len(item.Type) >= 1 && item.Type[0] == "h-feed" {
			var feedAuthor map[string]string

			if author, e := item.Properties["author"]; e && len(author) > 0 {
				feedAuthor, _ = simplifyCard(author)
			}
			for _, childItem := range item.Children {
				newItem := SimplifyMicroformat(childItem, feedAuthor)
				items = append(items, newItem)
			}
			return items
		}

		newItem := SimplifyMicroformat(item, nil)
		if newItem["type"] == "entry" || newItem["type"] == "event" || newItem["type"] == "card" {
			items = append(items, newItem)
		}
		if c, e := newItem["children"]; e {
			if ar, ok := c.([]map[string]interface{}); ok {
				for _, item := range ar {
					if item["type"] == "entry" || item["type"] == "event" || item["type"] == "card" {
						items = append(items, item)
					}
				}
			}
			delete(newItem, "children")
		}
	}
	return items
}

func fetchValue(key string, values map[string]string) string {
	if value, e := values[key]; e {
		return value
	}
	return ""
}
func MapToAuthor(result map[string]string) *microsub.Card {
	item := &microsub.Card{}
	item.Type = "card"
	item.Name = fetchValue("name", result)
	item.URL = fetchValue("url", result)
	item.Photo = fetchValue("photo", result)
	item.Longitude = fetchValue("longitude", result)
	item.Latitude = fetchValue("latitude", result)
	item.CountryName = fetchValue("country-name", result)
	item.Locality = fetchValue("locality", result)
	return item
}

func MapToItem(result map[string]interface{}) microsub.Item {
	item := microsub.Item{}

	item.Type = "entry"

	if itemType, e := result["type"]; e {
		item.Type = itemType.(string)
	}

	if name, e := result["name"]; e {
		item.Name = name.(string)
	}

	if url, e := result["url"]; e {
		item.URL = url.(string)
	}

	if uid, e := result["uid"]; e {
		item.UID = uid.(string)
	}

	if authorValue, e := result["author"]; e {
		if author, ok := authorValue.(map[string]string); ok {
			item.Author = MapToAuthor(author)
		}
	}

	if checkinValue, e := result["checkin"]; e {
		if checkin, ok := checkinValue.(map[string]string); ok {
			item.Checkin = MapToAuthor(checkin)
		}
	}

	if refsValue, e := result["refs"]; e {
		if refs, ok := refsValue.(map[string]interface{}); ok {
			item.Refs = make(map[string]microsub.Item)

			for key, ref := range refs {
				refItem := MapToItem(ref.(map[string]interface{}))
				refItem.Type = "entry"
				item.Refs[key] = refItem
			}
		}
	}

	if summary, e := result["summary"]; e {
		if summaryString, ok := summary.(string); ok {
			item.Summary = append(item.Summary, summaryString)
		}
	}

	if content, e := result["content"]; e {
		itemContent := &microsub.Content{}
		set := false
		if c, ok := content.(map[string]interface{}); ok {
			if html, e2 := c["html"]; e2 {
				itemContent.HTML = html.(string)
				set = true
			}
			if text, e2 := c["text"]; e2 {
				itemContent.Text = text.(string)
				set = true
			}
		}
		if set {
			item.Content = itemContent
		}
	}

	// TODO: Check how to improve this

	if value, e := result["like-of"]; e {
		if likeOf, ok := value.(string); ok {
			item.LikeOf = append(item.LikeOf, likeOf)
		} else if likeOfs, ok := value.([]interface{}); ok {
			for _, v := range likeOfs {
				if u, ok := v.(string); ok {
					item.LikeOf = append(item.LikeOf, u)
				}
			}
		}
	}

	if value, e := result["repost-of"]; e {
		if repost, ok := value.(string); ok {
			item.RepostOf = append(item.RepostOf, repost)
		} else if repost, ok := value.([]interface{}); ok {
			for _, v := range repost {
				if u, ok := v.(string); ok {
					item.RepostOf = append(item.RepostOf, u)
				}
			}
		}
	}

	if value, e := result["bookmark-of"]; e {
		if bookmark, ok := value.(string); ok {
			item.BookmarkOf = append(item.BookmarkOf, bookmark)
		} else if bookmarks, ok := value.([]interface{}); ok {
			for _, v := range bookmarks {
				if u, ok := v.(string); ok {
					item.BookmarkOf = append(item.BookmarkOf, u)
				}
			}
		}
	}

	if value, e := result["in-reply-to"]; e {
		if replyTo, ok := value.(string); ok {
			item.InReplyTo = append(item.InReplyTo, replyTo)
		} else if valueArray, ok := value.([]interface{}); ok {
			for _, v := range valueArray {
				if replyTo, ok := v.(string); ok {
					item.InReplyTo = append(item.InReplyTo, replyTo)
				} else if cite, ok := v.(map[string]interface{}); ok {
					item.InReplyTo = append(item.InReplyTo, cite["url"].(string))
				}
			}
		}
	}

	if value, e := result["photo"]; e {
		for _, v := range value.([]interface{}) {
			item.Photo = append(item.Photo, v.(string))
		}
	}

	if value, e := result["category"]; e {
		if cats, ok := value.([]string); ok {
			for _, v := range cats {
				item.Category = append(item.Category, v)
			}
		} else if cats, ok := value.([]interface{}); ok {
			for _, v := range cats {
				if cat, ok := v.(string); ok {
					item.Category = append(item.Category, cat)
				} else if cat, ok := v.(map[string]interface{}); ok {
					item.Category = append(item.Category, cat["value"].(string))
				}
			}
		} else if cat, ok := value.(string); ok {
			item.Category = append(item.Category, cat)
		}
	}

	if published, e := result["published"]; e {
		item.Published = published.(string)
	} else {
		item.Published = time.Now().Format(time.RFC3339)
	}

	if updated, e := result["updated"]; e {
		item.Updated = updated.(string)
	}

	if id, e := result["_id"]; e {
		item.ID = id.(string)
	}
	if read, e := result["_is_read"]; e {
		item.Read = read.(bool)
	}

	return item
}
