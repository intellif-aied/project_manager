package sessionsync

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestTeamPrepareRoutesNewSessionAndReusesPersonalSessionIntegration(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	actorID := int64(990101)
	ownerID := int64(990102)
	cleanupTeamSyncIntegration(t, database, actorID, ownerID)
	var teamID string
	if err := database.QueryRow(`INSERT INTO teams (name) VALUES ($1) RETURNING id`, "team-sync-integration-990101").Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO users (id, username, team_id) VALUES
			($1, 'team-sync-actor', $3), ($2, 'team-sync-owner', $3)`, actorID, ownerID, teamID); err != nil {
		t.Fatal(err)
	}
	defer cleanupTeamSyncIntegration(t, database, actorID, ownerID)
	if _, err := database.Exec(`
		INSERT INTO team_sync_paths (team_id, user_id, normalized_path) VALUES
			($1, $2, '/workspace/actor'), ($1, $3, '/workspace/owner')`, teamID, actorID, ownerID); err != nil {
		t.Fatal(err)
	}

	service, err := NewSyncService(database)
	if err != nil {
		t.Fatal(err)
	}
	personal := integrationPrepareRequest("existing-personal", []byte("{}\n"))
	personal.CWD = "/workspace/owner/project"
	personalPrepared, err := service.Prepare(context.Background(), fmt.Sprint(ownerID), personal)
	if err != nil {
		t.Fatal(err)
	}

	teamPrepared, err := service.PrepareWithMode(context.Background(), fmt.Sprint(actorID), UploadModeTeam, personal)
	if err != nil {
		t.Fatal(err)
	}
	if len(teamPrepared) != 1 || teamPrepared[0].GenerationID != personalPrepared[0].GenerationID {
		t.Fatalf("teamPrepared=%+v personalPrepared=%+v", teamPrepared, personalPrepared)
	}
	var existingOwner, uploadTeamID string
	if err := database.QueryRow(`
		SELECT s.user_id, g.upload_team_id
		FROM sessions s
		JOIN session_sources src ON src.session_id = s.id
		JOIN session_source_generations g ON g.id = src.staging_generation_id
		WHERE s.agent_type = $1 AND s.session_ref = $2`, personal.AgentType, personal.SessionRef,
	).Scan(&existingOwner, &uploadTeamID); err != nil {
		t.Fatal(err)
	}
	if existingOwner != fmt.Sprint(ownerID) || uploadTeamID != teamID {
		t.Fatalf("existing owner=%s upload team=%s", existingOwner, uploadTeamID)
	}
	if _, err := service.GenerationStatus(context.Background(), fmt.Sprint(actorID), teamPrepared[0].GenerationID); err != nil {
		t.Fatalf("team actor cannot read reused generation: %v", err)
	}

	created := integrationPrepareRequest("new-team-session", []byte("{}\n"))
	created.CWD = "/workspace/actor/project"
	createdPrepared, err := service.PrepareWithMode(context.Background(), fmt.Sprint(ownerID), UploadModeTeam, created)
	if err != nil || len(createdPrepared) != 1 || createdPrepared[0].Action != PrepareRebuildRequired {
		t.Fatalf("createdPrepared=%+v err=%v", createdPrepared, err)
	}
	var createdOwner string
	if err := database.QueryRow(`SELECT user_id FROM sessions WHERE agent_type = $1 AND session_ref = $2`, created.AgentType, created.SessionRef).Scan(&createdOwner); err != nil {
		t.Fatal(err)
	}
	if createdOwner != fmt.Sprint(actorID) {
		t.Fatalf("new session owner=%s want=%d", createdOwner, actorID)
	}

	unmapped := integrationPrepareRequest("unmapped-team-session", []byte("{}\n"))
	unmapped.CWD = "/workspace/unmapped"
	unmappedPrepared, err := service.PrepareWithMode(context.Background(), fmt.Sprint(actorID), UploadModeTeam, unmapped)
	if err != nil || len(unmappedPrepared) != 1 || unmappedPrepared[0].Action != PrepareRejected || unmappedPrepared[0].ErrorCode != ErrorTeamDirectoryUnmapped {
		t.Fatalf("unmappedPrepared=%+v err=%v", unmappedPrepared, err)
	}
	var unmappedCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sessions WHERE session_ref = $1`, unmapped.SessionRef).Scan(&unmappedCount); err != nil {
		t.Fatal(err)
	}
	if unmappedCount != 0 {
		t.Fatalf("unmapped session count=%d", unmappedCount)
	}
	if _, err := database.Exec(`UPDATE users SET team_id = NULL WHERE id = $1`, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GenerationStatus(context.Background(), fmt.Sprint(actorID), teamPrepared[0].GenerationID); err != ErrTeamContextChanged {
		t.Fatalf("generation remained writable after owner team migration: %v", err)
	}
}

func TestTeamPrepareRejectsDuplicateIdentityIntegration(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	firstID := int64(990103)
	secondID := int64(990104)
	cleanupTeamSyncIntegration(t, database, firstID, secondID)
	var teamID string
	if err := database.QueryRow(`INSERT INTO teams (name) VALUES ($1) RETURNING id`, "team-sync-integration-990103").Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO users (id, username, team_id) VALUES
			($1, 'team-sync-first', $3), ($2, 'team-sync-second', $3)`, firstID, secondID, teamID); err != nil {
		t.Fatal(err)
	}
	defer cleanupTeamSyncIntegration(t, database, firstID, secondID)
	if _, err := database.Exec(`INSERT INTO team_sync_paths (team_id, user_id, normalized_path) VALUES ($1, $2, '/workspace/duplicate')`, teamID, firstID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at, last_activity_at)
		VALUES ('duplicate-team-ref', $1, 'codex', now(), now()),
		       ('duplicate-team-ref', $2, 'codex', now(), now())`, firstID, secondID); err != nil {
		t.Fatal(err)
	}
	service, _ := NewSyncService(database)
	request := integrationPrepareRequest("duplicate-team-ref", []byte("{}\n"))
	request.AgentType = "codex"
	request.CWD = "/workspace/duplicate/project"
	prepared, err := service.PrepareWithMode(context.Background(), fmt.Sprint(firstID), UploadModeTeam, request)
	if err != nil || len(prepared) != 1 || prepared[0].ErrorCode != ErrorTeamSessionIdentityConflict {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
}

func TestTeamActorCanCommitChunkForMappedOwnerIntegration(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	actorID := int64(990105)
	ownerID := int64(990106)
	cleanupTeamSyncIntegration(t, database, actorID, ownerID)
	var teamID string
	if err := database.QueryRow(`INSERT INTO teams (name) VALUES ($1) RETURNING id`, "team-sync-integration-990105").Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO users (id, username, team_id) VALUES
			($1, 'team-chunk-actor', $3), ($2, 'team-chunk-owner', $3)`, actorID, ownerID, teamID); err != nil {
		t.Fatal(err)
	}
	defer cleanupTeamSyncIntegration(t, database, actorID, ownerID)
	if _, err := database.Exec(`
		INSERT INTO team_sync_paths (team_id, user_id, normalized_path)
		VALUES ($1, $2, '/workspace/team-chunk-owner')`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}

	service, err := NewSyncService(database)
	if err != nil {
		t.Fatal(err)
	}
	request := integrationPrepareRequest("team-chunk-session", []byte("{\"event\":1}\n"))
	request.CWD = "/workspace/team-chunk-owner/project"
	prepared, err := service.PrepareWithMode(context.Background(), fmt.Sprint(actorID), UploadModeTeam, request)
	if err != nil || len(prepared) != 1 || prepared[0].GenerationID == "" {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}

	repository, err := NewPostgresChunkRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	acceptor, err := NewChunkAcceptor(repository, &fakeVerifiedStore{})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("{\"event\":1}\n")
	decision, err := acceptor.Accept(
		context.Background(),
		integrationAcceptRequest(actorID, prepared[0].GenerationID, 0, 1, content),
	)
	if err != nil || decision.Status != ChunkAccepted {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	if _, err := database.Exec(`
		UPDATE sessions
		SET content_status = 'upload_failed'
		WHERE id = (
			SELECT src.session_id
			FROM session_source_generations g
			JOIN session_sources src ON src.id = g.source_id
			WHERE g.id = $1
		)`, prepared[0].GenerationID); err != nil {
		t.Fatal(err)
	}
	finalized, err := service.Finalize(context.Background(), fmt.Sprint(actorID), prepared[0].GenerationID, FinalizeRequest{
		DeclaredEndCursor:                int64(len(content)),
		PrefixCheckpointHash:             HashBytes(content),
		PrefixCheckpointAlgorithmVersion: PrefixCheckpointAlgorithm,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.ContentStatus == ContentUploadFailed {
		t.Fatalf("finalize kept stale content status: %+v", finalized)
	}
	status, err := service.GenerationStatus(context.Background(), fmt.Sprint(actorID), prepared[0].GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if status.ContentStatus == ContentUploadFailed || status.ErrorCode != "" {
		t.Fatalf("status reported a successful replacement as failed: %+v", status)
	}
}

func cleanupTeamSyncIntegration(t *testing.T, database *sql.DB, userIDs ...int64) {
	t.Helper()
	for _, userID := range userIDs {
		_, _ = database.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
		_, _ = database.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}
	_, _ = database.Exec(`DELETE FROM teams WHERE name LIKE 'team-sync-integration-%'`)
}
