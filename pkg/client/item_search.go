package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// ItemSearchWithOptions searches content and returns paging cursors. Values are
// sent in the POST form so repeated filter values remain distinct.
func (c *Client) ItemSearchWithOptions(ctx context.Context, o microsub.ItemSearchOptions) (microsub.Timeline, error) {
	if err := microsub.ValidateItemSearchOptions(o); err != nil {
		return microsub.Timeline{}, err
	}
	v := url.Values{"channel": {o.Channel}}
	if o.Query != "" {
		v.Set("query", o.Query)
	}
	for k, values := range map[string][]string{"author": o.Author, "category": o.Category, "kind": o.Kind, "source": o.Source} {
		for _, value := range values {
			v.Add(k, value)
		}
	}
	if o.Before != "" {
		v.Set("before", o.Before)
	}
	if o.After != "" {
		v.Set("after", o.After)
	}
	if o.Limit > 0 {
		v.Set("limit", strconv.Itoa(o.Limit))
	}
	res, err := c.microsubPostFormRequest(ctx, "search", nil, v)
	if err != nil {
		return microsub.Timeline{}, err
	}
	defer res.Body.Close()
	var body struct {
		microsub.Timeline
		Error string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return microsub.Timeline{}, err
	}
	if body.Error != "" {
		return microsub.Timeline{}, fmt.Errorf("search: %s", body.Error)
	}
	if body.Items == nil {
		body.Items = []microsub.Item{}
	}
	return body.Timeline, nil
}
