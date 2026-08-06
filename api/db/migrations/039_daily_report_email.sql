CREATE TABLE report_email_recipients (
    user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    email        TEXT NOT NULL,
    source       TEXT NOT NULL DEFAULT 'enterprise_directory',
    enabled      BOOLEAN NOT NULL DEFAULT true,
    verified_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (email = lower(btrim(email)) AND email <> ''),
    CHECK (source = 'enterprise_directory')
);

CREATE UNIQUE INDEX idx_report_email_recipients_email
    ON report_email_recipients (lower(email));

CREATE TABLE report_email_deliveries (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_date         DATE NOT NULL,
    delivery_type       TEXT NOT NULL CHECK (delivery_type IN ('personal', 'team_summary')),
    recipient_user_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id             UUID REFERENCES teams(id) ON DELETE SET NULL,
    recipient_email     TEXT NOT NULL DEFAULT '',
    subject             TEXT NOT NULL DEFAULT '',
    text_body           TEXT NOT NULL DEFAULT '',
    html_body           TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'skipped')),
    attempts            INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0 AND attempts <= 3),
    next_attempt_at     TIMESTAMPTZ,
    lease_owner         TEXT,
    lease_until         TIMESTAMPTZ,
    last_error          TEXT,
    sent_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (report_date, delivery_type, recipient_user_id)
);

CREATE INDEX idx_report_email_deliveries_claim
    ON report_email_deliveries (status, next_attempt_at, created_at)
    WHERE status IN ('pending', 'failed', 'sending');
