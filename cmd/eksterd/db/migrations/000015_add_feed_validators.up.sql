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
 * Fetching a feed politely needs memory: the validators the server handed out
 * with the last body, and how many times in a row the fetch has failed.
 *
 * etag and last_modified are sent back as If-None-Match and
 * If-Modified-Since, so an unchanged feed costs the host a comparison instead
 * of a download. consecutive_failures drives the retry schedule; without it a
 * failing feed is retried on every ticker tick, which is the opposite of
 * polite.
 */

ALTER TABLE "feeds" ADD COLUMN "etag" text;
ALTER TABLE "feeds" ADD COLUMN "last_modified" text;
ALTER TABLE "feeds" ADD COLUMN "consecutive_failures" integer NOT NULL DEFAULT 0;
