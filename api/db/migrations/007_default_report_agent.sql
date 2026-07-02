ALTER TABLE managed_agent_profiles
    ADD COLUMN IF NOT EXISTS is_default_report BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_agent_profiles_default_report
    ON managed_agent_profiles(user_id)
    WHERE is_default_report = true;
