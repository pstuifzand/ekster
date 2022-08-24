ALTER TABLE "channels"
    DROP CONSTRAINT "channels_name_key",
    ADD CONSTRAINT "channels_name_user_id_key" UNIQUE ("name", "user_id");
