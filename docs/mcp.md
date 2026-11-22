# Ek MCP server

`ek mcp` starts a standard MCP server over stdio for Ek's one configured
Microsub account. Run `ek connect <website-url>` first. The server uses the
existing saved token and endpoint; it does not print protocol data or tokens to
logs/stdout. Diagnostics go to stderr.

The command uses the official Go MCP SDK v0.8.0, compatible with this
repository's Go 1.23 baseline. Tools use inferred, validated JSON input schemas,
explicit typed output schemas, and structured tool results (`structuredContent`).

## Tools

Read tools: `list_channels`, `get_timeline`, `search_feeds`, `search_items`,
`list_follows`, and `preview_url`. `search_items` supports structured filters and
paging; see [content search](content-search.md) for fields and examples.

Mutation tools: `create_channel`, `update_channel`, `delete_channel`, `order_channels`,
`follow_url`, `unfollow_url`, `mark_read`, `mark_read_through`, `mark_unread`, and `remove_entries`. These operate on the configured
Microsub account; destructive and state-changing operations are annotated as
non-read-only, but MCP annotations are not a security boundary.

## Example MCP client configuration

For an MCP client that accepts a `mcpServers` configuration:

```json
{
  "mcpServers": {
    "ek-microsub": {
      "command": "/absolute/path/to/ek",
      "args": ["mcp"]
    }
  }
}
```

The client must launch Ek as a subprocess and connect using stdio transport.

`remove_entries` deletes the selected channel’s stored entries. A later feed refresh may reintroduce them; it does not unfollow the feed or delete the source post.
