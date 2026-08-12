ALTER TABLE automatic_turn_runs
    ADD COLUMN tts_profile_id TEXT,
    ADD CONSTRAINT automatic_turn_runs_tts_profile_not_empty
        CHECK (tts_profile_id IS NULL OR tts_profile_id <> '');

CREATE OR REPLACE FUNCTION recordstore_reject_automatic_turn_run_tts_profile_updates()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.tts_profile_id IS DISTINCT FROM OLD.tts_profile_id THEN
        RAISE EXCEPTION 'automatic turn run TTS profile cannot be updated';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER automatic_turn_runs_reject_tts_profile_updates
    BEFORE UPDATE ON automatic_turn_runs
    FOR EACH ROW
    EXECUTE FUNCTION recordstore_reject_automatic_turn_run_tts_profile_updates();
