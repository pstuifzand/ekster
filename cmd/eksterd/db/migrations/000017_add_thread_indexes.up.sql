/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

/*
 * A thread asks "items" two questions, and both are about a value nested in
 * the JSON document:
 *
 *   - the entry with this permalink:             data->>'url' = $1
 *   - the entries answering these permalinks:    (data->'in-reply-to') ?| $1
 *
 * An index over the whole document (USING GIN ("data")) does NOT answer the
 * second one: the operator's left side is a nested value, not "data" itself,
 * so the planner falls back to a sequential scan of the table — measured, and
 * the reason these are expression indexes.
 *
 * The url lookup is a btree on a text expression: it holds one value per row
 * and is a fraction of the size of a GIN over the document. The replies lookup
 * needs GIN because it is an array membership test; "jsonb_ops" (the default)
 * is required, as "jsonb_path_ops" only supports the containment operator.
 *
 * golang-migrate sends a migration file as one simple query, so neither index
 * can be CREATE INDEX CONCURRENTLY.
 */

CREATE INDEX IF NOT EXISTS "items_url_idx"
    ON "items" ((data->>'url'));

CREATE INDEX IF NOT EXISTS "items_in_reply_to_gin_idx"
    ON "items" USING GIN ((data->'in-reply-to'));
