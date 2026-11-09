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

ALTER TABLE "feeds" DROP COLUMN IF EXISTS "last_item_count";
ALTER TABLE "feeds" DROP COLUMN IF EXISTS "last_success_at";
ALTER TABLE "feeds" DROP COLUMN IF EXISTS "last_error";
ALTER TABLE "feeds" DROP COLUMN IF EXISTS "last_http_status";
ALTER TABLE "feeds" DROP COLUMN IF EXISTS "last_fetch_at";
