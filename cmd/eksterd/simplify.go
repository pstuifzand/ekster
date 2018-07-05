package main

import (
	"strings"

	"willnorris.com/go/microformats"
)

func simplify(itemType string, item map[string][]interface{}) map[string]interface{} {
	feedItem := make(map[string]interface{})

	for k, v := range item {
		if k == "bookmark-of" || k == "like-of" || k == "repost-of" || k == "in-reply-to" {
			if value, ok := v[0].(*microformats.Microformat); ok {
				feedItem[k] = value.Value
			} else {
				feedItem[k] = v
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
			if value, ok := v[0].(*microformats.Microformat); ok {
				card := make(map[string]string)
				card["type"] = "card"
				for ik, vk := range value.Properties {
					if p, ok := vk[0].(string); ok {
						card[ik] = p
					}
				}
				feedItem[k] = card
			}
		} else if value, ok := v[0].(*microformats.Microformat); ok {
			mType := value.Type[0][2:]
			m := simplify(mType, value.Properties)
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

	return feedItem
}

func simplifyMicroformat(item *microformats.Microformat) map[string]interface{} {
	itemType := item.Type[0][2:]
	newItem := simplify(itemType, item.Properties)
	newItem["type"] = itemType

	children := []map[string]interface{}{}

	if len(item.Children) > 0 {
		for _, c := range item.Children {
			child := simplifyMicroformat(c)
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

func simplifyMicroformatData(md *microformats.Data) []map[string]interface{} {
	items := []map[string]interface{}{}
	for _, item := range md.Items {
		if len(item.Type) >= 1 && item.Type[0] == "h-feed" {
			for _, childItem := range item.Children {
				newItem := simplifyMicroformat(childItem)
				items = append(items, newItem)
			}
			return items
		}

		newItem := simplifyMicroformat(item)
		items = append(items, newItem)
		if c, e := newItem["children"]; e {
			if ar, ok := c.([]map[string]interface{}); ok {
				items = append(items, ar...)
			}
			delete(newItem, "children")
		}
	}
	return items
}
