package timeline

import "p83.nl/go/ekster/pkg/microsub"

type nullTimeline struct {
	channel string
}

func (timeline *nullTimeline) Init() error {
	return nil
}

func (timeline *nullTimeline) Items(before, after string) (microsub.Timeline, error) {
	return microsub.Timeline{Items: []microsub.Item{}}, nil
}

func (timeline *nullTimeline) AddItem(item microsub.Item) (bool, error) {
	return false, nil
}

func (timeline *nullTimeline) Count() (int, error) {
	return 0, nil
}

func (timeline *nullTimeline) MarkRead(uids []string) error {
	return nil
}
