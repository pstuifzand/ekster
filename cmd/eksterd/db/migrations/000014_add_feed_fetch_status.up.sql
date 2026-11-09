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
 * A feed that breaks is invisible today: the refresh loop only logs, and an
 * empty timeline looks the same as a quiet blog. These columns hold the outcome
 * of the last fetch so /status can name the feeds that are failing instead of
 * requiring a manual audit.
 *
 * last_http_status is NULL when the request never produced a response (DNS,
 * TLS, timeout), which is what separates "the site is gone" from "the site
 * answers 404".
 */

ALTER TABLE "feeds" ADD COLUMN "last_fetch_at" timestamp with time zone;
ALTER TABLE "feeds" ADD COLUMN "last_http_status" integer;
ALTER TABLE "feeds" ADD COLUMN "last_error" text;
ALTER TABLE "feeds" ADD COLUMN "last_success_at" timestamp with time zone;
ALTER TABLE "feeds" ADD COLUMN "last_item_count" integer;
