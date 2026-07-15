DROP INDEX IF EXISTS idx_session_daily_usage_one_current_group;

CREATE UNIQUE INDEX idx_session_daily_usage_one_current_group
    ON session_daily_usage(
        revision_id, session_id, user_id,
        COALESCE(team_id_snapshot, '00000000-0000-0000-0000-000000000000'::UUID),
        COALESCE(department_id_snapshot, '00000000-0000-0000-0000-000000000000'::UUID),
        activity_date, provider, COALESCE(canonical_model, ''), billing_variant
    ) WHERE valid_to IS NULL;
