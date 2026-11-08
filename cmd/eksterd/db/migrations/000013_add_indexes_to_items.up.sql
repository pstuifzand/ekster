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
 * Postgres never indexes a foreign key for you. Without these two indexes the
 * channels query performed one sequential scan of "items" per channel
 * (loops=N in the plan) to count unread items, and the timeline query sorted
 * the whole table to return twenty rows.
 *
 * golang-migrate sends a migration file as one simple query, so these cannot be
 * CREATE INDEX CONCURRENTLY. At the size this table is (hundreds of thousands
 * of rows) a plain build holds a SHARE lock for well under a second.
 */

CREATE INDEX IF NOT EXISTS "items_channel_unread_idx"
    ON "items" ("channel_id")
    WHERE "is_read" = 0;

CREATE INDEX IF NOT EXISTS "items_channel_published_idx"
    ON "items" ("channel_id", "published_at" DESC);
