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
`list_follows`, and `preview_url`.

Mutation tools: `create_channel`, `update_channel`, `delete_channel`,
`follow_url`, `unfollow_url`, and `mark_read`. These operate on the configured
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
