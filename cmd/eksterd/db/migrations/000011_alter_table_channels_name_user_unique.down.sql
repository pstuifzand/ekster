ALTER TABLE "channels"
    DROP CONSTRAINT "channels_name_user_id_key",
    ADD CONSTRAINT "channels_name_key" UNIQUE ("name");
