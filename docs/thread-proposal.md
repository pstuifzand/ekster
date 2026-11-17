# Thread: design rationale

The specification of this extension is `docs/extensions/thread.md`. This note
keeps the reasoning behind it, because most of the decisions were choices
between two defensible options and the reasons are worth not re-deriving.

## What belongs in the protocol

The test applied throughout: add the reads only a server can perform, and
refuse the writes another protocol already owns.

- Microsub's deficiency was never the social verbs. The item types exist
  (`like-of`, `repost-of`, `bookmark-of`, `in-reply-to`, `mention-of`), the
  jf2 mapping exists, and channels already infer the kind for `exclude_type`.
  What was missing is that the model can hold a post but not the conversation
  it sits in.
- Thread passes the test: the client provably cannot assemble it, because the
  participants of a conversation are exactly the posts the user does not
  follow. It needs no identity, no social graph, no write path.
- Likes, reposts, follows, ranking and DMs fail it. They are Micropub's or
  Webmention's, or nobody's. Adding them here would make Microsub a second and
  worse Micropub.

## Why a timeline method and not a new action

`action=thread` would have been cleaner to read, but `timeline` already has a
`method` parameter and that is what the extension point is for. Reusing it means
a server without the extension answers 400 from the branch that already rejects
unknown methods, instead of needing a second unknown-action path. The first
version of this proposal said `action=thread`; what is implemented is
`action=timeline&method=thread`.

Advertise by probing. Microsub has no capability declaration, so a client sends
the request and treats 400 as "not supported" — the same shape it needs for any
optional action.

## Why return directly instead of searching

A search over `kind=reply` plus the parent URL returns "replies from feeds I
follow", and the client will draw that as "the conversation" with no way to tell
the difference. A thread is a bounded, ordered tree; search results are a
ranked, paginated page. Modelling one as the other builds a false completeness
claim into the response.

Live-fetching at request time is the other wrong answer: N outbound requests
inside a GET, unbounded depth, a handler that hangs on one slow blog. Reference
enrichment already resolves one hop *in the fetch pipeline*, and that is where
it belongs.

## Why ancestors are fetched and replies are not

Ascending is a finite, knowable chain: each item names its parent. Descending is
unbounded and unknown — nothing tells a server it has found all the replies, and
crawling for them would make it a search engine rather than a reader. So
ancestors walk, replies are whatever is already indexed, and `thread.partial`
tells the truth about the difference.

## Why fetched ancestors are cached, not stored

The first version of this note proposed storing a fetched ancestor as an item
row with `channel_id IS NULL`, so every thread that reached it would reuse one
row.

That is wrong in this schema. `items.uid` is unique across the instance and the
insert is `ON CONFLICT (uid) DO UPDATE`, which updates `data` and `feed_id` but
never `channel_id`. A user who later follows the feed that published that
ancestor would find the entry updating a channelless row, and it would never
appear in a timeline — the exact bug class that already exists for other
reasons. So the document is cached (in the HTTP cache, keyed by URL, with a
TTL) and no item row is created. One row per URL stays the right design for the
future, but only after items are keyed per channel.

## Why an ancestor without microformats still shows up

The first live test was the useful one: a post replying to
`keningzhu.com/journal/contact-me-ripple` came back as `partial, 1 ancestor(s)
not shown`. The parent page is a Squarespace page — 200, `text/html`, no
h-entry anywhere. `itemAtURL` required a published entry, so the one half of
the feature that matters most, the parent, produced nothing.

Microformats are the exception on the web, not the rule: a blog on Squarespace,
Medium or a static generator publishes `og:title` and a description and nothing
else. So an ancestor that parses to no entries falls back to the page's own
metadata: `og:title` or `<title>` as the name, `og:description` or the meta
description as text content, `og:image` as the photo. Bounded in length,
decoded by the HTML parser, and text-only — the reader escapes it, so a thread
never becomes a sanitizing problem.

That is a card, not a post. It is still better than "1 ancestor not shown",
because the reader can now link the actual conversation.

## Why references are separate from the conversation

The second live complaint was the same shape one level out: an entry whose
`mention-of` names another post came back as a thread of one. The entry is not
answering anything — there is no `in-reply-to` — but it is *about* something,
and a reader shows that post or shows nothing.

Putting the mentioned post in the chain would be wrong: a mention has no
direction and no order, and `thread.root` would then claim a parent that does
not exist. So `refs` is a separate map, keyed by the URL the property names,
resolved the same way an ancestor is (indexed copy first, then one bounded
fetch) and capped. A like, a repost and a bookmark are the same situation and
come through the same map; the anchor's own arrays tell a client which is
which.

## Why the indexes are expression indexes

Measured on a test instance: `USING GIN ("data")` answers the permalink lookup
but not the replies query, because the operator's left side is
`data->'in-reply-to'`, a nested value rather than the indexed column. The
planner fell back to a sequential scan even with `enable_seqscan=off`. Two
expression indexes answer both, and the btree over `data->>'url'` is a fraction
of the size of a GIN over the whole document.

## What was deliberately not built

- `_is_starred` / save-for-later. A bookmark is a Micropub post and comes back
  in the timeline if you follow your own site; the outbox is the store. Worth
  revisiting only if a client asks for private state.
- `_is_liked` / `_is_shared` on items, joined from the user's own outbox. Good
  idea, separate work, and it needs a policy for whose outbox a server reads.
- `kind=`, `author=` and `source=` filters on search. The inference already
  exists for exclusions; wiring it to selection is a small, independent change.
- A merged `channel=global` timeline. The spec already defines that value for
  content search, so it is an extension of a name the protocol already owns.

## Open questions

- Should `depth` apply to replies as well as ancestors? Currently it bounds the
  walk up only; replies are bounded by item count and level instead.
- Is `thread.missing` worth the maintenance cost, or is `partial` enough? It is
  cheap here (one counter), and it is what lets a client say "2 earlier posts".
- Should a server return a thread root it fetched on an earlier request but
  never subscribed to? Yes, while the cache entry lives.
