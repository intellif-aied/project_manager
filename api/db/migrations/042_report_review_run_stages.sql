ALTER TABLE ai_runs
    DROP CONSTRAINT ai_runs_execution_stage_check,
    DROP CONSTRAINT ai_runs_report_stage_status_check;

ALTER TABLE ai_runs
    ADD CONSTRAINT ai_runs_execution_stage_check CHECK (
        execution_stage IS NULL OR execution_stage IN (
            'waiting_digest', 'building_context', 'submitting_agent',
            'agent_running', 'review_pending', 'review_running',
            'review_finalizing', 'writing_result', 'completed'
        )
    ),
    ADD CONSTRAINT ai_runs_report_stage_status_check CHECK (
        business_type <> 'report_agent_run' OR execution_stage IS NULL OR
        (execution_stage IN ('waiting_digest', 'building_context', 'submitting_agent') AND status = 'pending') OR
        (execution_stage IN (
            'agent_running', 'review_pending', 'review_running',
            'review_finalizing', 'writing_result'
        ) AND status = 'running') OR
        (execution_stage = 'completed' AND status IN ('succeeded', 'failed', 'timeout'))
    );
