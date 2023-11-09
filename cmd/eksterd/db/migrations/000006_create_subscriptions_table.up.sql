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

CREATE TABLE "subscriptions" (
     "id" int primary key generated always as identity,
     "topic" varchar(1024) not null references "feeds" ("url") on update cascade on delete cascade,
     "hub" varchar(1024) null,
     "callback" varchar(1024) null,
     "subscription_secret" varchar(32) not null,
     "url_secret" varchar(32) not null,
     "lease_seconds" int not null,
     "created_at" timestamptz DEFAULT current_timestamp,
     "updated_at" timestamptz,
     "resubscribe_at" timestamptz
);
