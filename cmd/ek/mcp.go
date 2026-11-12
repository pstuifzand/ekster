package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

type mcpTools struct {
	microsub *client.Client
}

type emptyInput struct{}
type timelineInput struct {
	Channel string  `json:"channel" jsonschema:"Microsub channel UID (use home for the default timeline)"`
	Before  *string `json:"before,omitempty" jsonschema:"Return items older than this paging cursor"`
	After   *string `json:"after,omitempty" jsonschema:"Return items newer than this paging cursor"`
}
type searchFeedsInput struct {
	Query string `json:"query" jsonschema:"Feed search text"`
}
type searchItemsInput struct {
	Query   string  `json:"query" jsonschema:"Text to search for in timeline items"`
	Channel *string `json:"channel,omitempty" jsonschema:"Channel UID to search; defaults to global"`
}
type channelNameInput struct {
	Name string `json:"name" jsonschema:"Channel name"`
}
type channelUpdateInput struct {
	Channel string `json:"channel" jsonschema:"Channel UID"`
	Name    string `json:"name" jsonschema:"New channel name"`
}
type channelInput struct {
	Channel string `json:"channel" jsonschema:"Channel UID"`
}
type followInput struct {
	Channel string `json:"channel" jsonschema:"Channel UID"`
	URL     string `json:"url" jsonschema:"Website or feed URL to follow"`
}
type previewInput struct {
	URL string `json:"url" jsonschema:"Website or feed URL to preview"`
}
type markReadInput struct {
	Channel string   `json:"channel" jsonschema:"Channel UID"`
	Entries []string `json:"entries" jsonschema:"Stable item IDs to mark read"`
}
type feedOutput struct {
	Feed microsub.Feed `json:"feed" jsonschema:"Followed feed"`
}
type channelOutput struct {
	Channel microsub.Channel `json:"channel" jsonschema:"Created or updated channel"`
}
type previewOutput struct {
	Items []itemResult `json:"items" jsonschema:"Preview timeline items"`
}
type successOutput struct {
	Success bool `json:"success" jsonschema:"Whether the operation succeeded"`
}
type followsOutput struct {
	Feeds []microsub.Feed `json:"feeds" jsonschema:"Feeds followed in the channel"`
}
type channelResult struct {
	UID         string `json:"uid" jsonschema:"Channel UID"`
	Name        string `json:"name" jsonschema:"Channel name"`
	UnreadCount int    `json:"unread_count" jsonschema:"Number of unread items"`
}
type channelsOutput struct {
	Channels []channelResult `json:"channels" jsonschema:"Available Microsub channels"`
}
type itemResult struct {
	ID          string `json:"id" jsonschema:"Stable item ID used for paging and marking read"`
	UID         string `json:"uid,omitempty" jsonschema:"Original item UID"`
	Type        string `json:"type" jsonschema:"Microformats item type"`
	Name        string `json:"name,omitempty" jsonschema:"Item title"`
	Published   string `json:"published,omitempty" jsonschema:"Publication timestamp"`
	URL         string `json:"url,omitempty" jsonschema:"Canonical item URL"`
	Author      string `json:"author,omitempty" jsonschema:"Author name"`
	AuthorURL   string `json:"author_url,omitempty" jsonschema:"Author URL"`
	ContentText string `json:"content_text,omitempty" jsonschema:"Plain text body"`
	ContentHTML string `json:"content_html,omitempty" jsonschema:"HTML body"`
	IsRead      bool   `json:"is_read" jsonschema:"Whether the item has been read"`
}
type pagingResult struct {
	Before string `json:"before,omitempty" jsonschema:"Cursor for older items"`
	After  string `json:"after,omitempty" jsonschema:"Cursor for newer items"`
}
type timelineOutput struct {
	Channel string       `json:"channel" jsonschema:"Requested channel UID"`
	Items   []itemResult `json:"items" jsonschema:"Timeline items"`
	Paging  pagingResult `json:"paging" jsonschema:"Pagination cursors"`
}
type feedsOutput struct {
	Feeds []microsub.Feed `json:"feeds" jsonschema:"Matching feeds"`
}
type itemsOutput struct {
	Items []itemResult `json:"items" jsonschema:"Matching items"`
}

func newMCPServer(c *client.Client) *mcp.Server {
	tools := &mcpTools{microsub: c}
	server := mcp.NewServer(&mcp.Implementation{Name: "ek-microsub", Title: "Ek Microsub", Version: Version}, nil)
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
	openSearch := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(true)}

	mcp.AddTool(server, &mcp.Tool{Name: "list_channels", Title: "List Microsub channels", Description: "List channels in the configured Microsub account, including unread counts.", Annotations: readOnly}, tools.listChannels)
	mcp.AddTool(server, &mcp.Tool{Name: "get_timeline", Title: "Get channel timeline", Description: "Fetch one page of items from a Microsub channel. Use returned paging.before or paging.after to fetch adjacent pages.", Annotations: readOnly}, tools.getTimeline)
	mcp.AddTool(server, &mcp.Tool{Name: "search_feeds", Title: "Search for feeds", Description: "Search the configured Microsub server for feeds matching a query.", Annotations: openSearch}, tools.searchFeeds)
	mcp.AddTool(server, &mcp.Tool{Name: "search_items", Title: "Search timeline items", Description: "Search items in a Microsub channel; searches the global timeline by default.", Annotations: readOnly}, tools.searchItems)
	write := &mcp.ToolAnnotations{ReadOnlyHint: false, OpenWorldHint: boolPtr(true)}
	mcp.AddTool(server, &mcp.Tool{Name: "create_channel", Description: "Create a channel.", Annotations: write}, tools.createChannel)
	mcp.AddTool(server, &mcp.Tool{Name: "update_channel", Description: "Rename a channel.", Annotations: write}, tools.updateChannel)
	mcp.AddTool(server, &mcp.Tool{Name: "delete_channel", Description: "Delete a channel and its subscriptions.", Annotations: write}, tools.deleteChannel)
	mcp.AddTool(server, &mcp.Tool{Name: "list_follows", Description: "List feeds followed in a channel.", Annotations: readOnly}, tools.listFollows)
	mcp.AddTool(server, &mcp.Tool{Name: "follow_url", Description: "Follow a website or feed URL in a channel.", Annotations: write}, tools.followURL)
	mcp.AddTool(server, &mcp.Tool{Name: "unfollow_url", Description: "Unfollow a URL in a channel.", Annotations: write}, tools.unfollowURL)
	mcp.AddTool(server, &mcp.Tool{Name: "preview_url", Description: "Preview a website or feed before following it.", Annotations: openSearch}, tools.previewURL)
	mcp.AddTool(server, &mcp.Tool{Name: "mark_read", Description: "Mark timeline items as read in a channel.", Annotations: write}, tools.markRead)
	return server
}

func runMCP(c *client.Client) error {
	server := newMCPServer(c)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, &mcp.StdioTransport{})
}

func boolPtr(v bool) *bool { return &v }

func (t *mcpTools) listChannels(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, channelsOutput, error) {
	channels, err := t.microsub.ChannelsGetList(ctx)
	if err != nil {
		return nil, channelsOutput{}, err
	}
	out := channelsOutput{Channels: make([]channelResult, 0, len(channels))}
	for _, channel := range channels {
		unread := channel.Unread.UnreadCount
		if channel.Unread.Type == microsub.UnreadBool && channel.Unread.Unread {
			unread = 1
		}
		out.Channels = append(out.Channels, channelResult{UID: channel.UID, Name: channel.Name, UnreadCount: unread})
	}
	return nil, out, nil
}

func (t *mcpTools) getTimeline(ctx context.Context, _ *mcp.CallToolRequest, input timelineInput) (*mcp.CallToolResult, timelineOutput, error) {
	before, after := "", ""
	if input.Before != nil {
		before = *input.Before
	}
	if input.After != nil {
		after = *input.After
	}
	timeline, err := t.microsub.TimelineGet(ctx, before, after, input.Channel)
	if err != nil {
		return nil, timelineOutput{}, err
	}
	return nil, timelineOutput{Channel: input.Channel, Items: mapItems(timeline.Items), Paging: pagingResult{Before: timeline.Paging.Before, After: timeline.Paging.After}}, nil
}

func (t *mcpTools) searchFeeds(ctx context.Context, _ *mcp.CallToolRequest, input searchFeedsInput) (*mcp.CallToolResult, feedsOutput, error) {
	feeds, err := t.microsub.Search(ctx, input.Query)
	if err != nil {
		return nil, feedsOutput{}, err
	}
	if feeds == nil {
		feeds = []microsub.Feed{}
	}
	return nil, feedsOutput{Feeds: feeds}, nil
}

func (t *mcpTools) searchItems(ctx context.Context, _ *mcp.CallToolRequest, input searchItemsInput) (*mcp.CallToolResult, itemsOutput, error) {
	channel := "global"
	if input.Channel != nil && *input.Channel != "" {
		channel = *input.Channel
	}
	items, err := t.microsub.ItemSearch(ctx, channel, input.Query)
	if err != nil {
		return nil, itemsOutput{}, err
	}
	return nil, itemsOutput{Items: mapItems(items)}, nil
}

func (t *mcpTools) createChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelNameInput) (*mcp.CallToolResult, channelOutput, error) {
	ch, err := t.microsub.ChannelsCreate(ctx, in.Name)
	return nil, channelOutput{Channel: ch}, err
}
func (t *mcpTools) updateChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelUpdateInput) (*mcp.CallToolResult, channelOutput, error) {
	ch, err := t.microsub.ChannelsUpdate(ctx, in.Channel, in.Name)
	return nil, channelOutput{Channel: ch}, err
}
func (t *mcpTools) deleteChannel(ctx context.Context, _ *mcp.CallToolRequest, in channelInput) (*mcp.CallToolResult, successOutput, error) {
	err := t.microsub.ChannelsDelete(ctx, in.Channel)
	return nil, successOutput{Success: err == nil}, err
}
func (t *mcpTools) listFollows(ctx context.Context, _ *mcp.CallToolRequest, in channelInput) (*mcp.CallToolResult, followsOutput, error) {
	feeds, err := t.microsub.FollowGetList(ctx, in.Channel)
	if feeds == nil {
		feeds = []microsub.Feed{}
	}
	return nil, followsOutput{Feeds: feeds}, err
}
func (t *mcpTools) followURL(ctx context.Context, _ *mcp.CallToolRequest, in followInput) (*mcp.CallToolResult, feedOutput, error) {
	feed, err := t.microsub.FollowURL(ctx, in.Channel, in.URL)
	return nil, feedOutput{Feed: feed}, err
}
func (t *mcpTools) unfollowURL(ctx context.Context, _ *mcp.CallToolRequest, in followInput) (*mcp.CallToolResult, successOutput, error) {
	err := t.microsub.UnfollowURL(ctx, in.Channel, in.URL)
	return nil, successOutput{Success: err == nil}, err
}
func (t *mcpTools) previewURL(ctx context.Context, _ *mcp.CallToolRequest, in previewInput) (*mcp.CallToolResult, previewOutput, error) {
	timeline, err := t.microsub.PreviewURL(ctx, in.URL)
	return nil, previewOutput{Items: mapItems(timeline.Items)}, err
}
func (t *mcpTools) markRead(ctx context.Context, _ *mcp.CallToolRequest, in markReadInput) (*mcp.CallToolResult, successOutput, error) {
	err := t.microsub.MarkRead(ctx, in.Channel, in.Entries)
	return nil, successOutput{Success: err == nil}, err
}

func mapItems(items []microsub.Item) []itemResult {
	out := make([]itemResult, 0, len(items))
	for _, item := range items {
		result := itemResult{ID: item.ID, UID: item.UID, Type: item.Type, Name: item.Name, Published: item.Published, URL: item.URL, IsRead: item.Read}
		if item.Author != nil {
			result.Author, result.AuthorURL = item.Author.Name, item.Author.URL
		}
		if item.Content != nil {
			result.ContentText, result.ContentHTML = item.Content.Text, item.Content.HTML
		}
		out = append(out, result)
	}
	return out
}
