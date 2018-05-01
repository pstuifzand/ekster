package feedbin

import (
	"fmt"
	"net/http"
)

func (fb *Feedbin) get(u string) (*http.Response, error) {

	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.feedbin.com%s", u), nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(fb.user, fb.password)

	client := http.Client{}

	return client.Do(req)
}
