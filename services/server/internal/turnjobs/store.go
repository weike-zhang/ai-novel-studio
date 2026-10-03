package turnjobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(30)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}

func (s *Store) Create(ctx context.Context, input CreateInput) (Job, bool, error) {
	if !ValidInteraction(input.Interaction) {
		input.Interaction = InteractionAct
	}
	input.Action = strings.TrimSpace(input.Action)
	if input.Interaction != InteractionContinue {
		if err := storyengine.ValidateAction(input.Action); err != nil {
			return Job{}, false, err
		}
	} else if input.Action != "" {
		return Job{}, false, fmt.Errorf("continue interaction must not carry an action")
	}
	if len(input.IdempotencyKey) < 16 || len(input.IdempotencyKey) > 128 {
		return Job{}, false, fmt.Errorf("invalid idempotency key")
	}
	if input.Credits <= 0 {
		return Job{}, false, fmt.Errorf("credits must be positive")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Job{}, false, fmt.Errorf("begin create job: %w", err)
	}
	defer tx.Rollback()

	existing, err := getJobByIdempotency(ctx, tx, input.UserID, input.IdempotencyKey)
	if err == nil {
		if existing.SimulationID != input.SimulationID || existing.Action != input.Action {
			return Job{}, false, ErrIdempotencyConflict
		}
		if turn, turnErr := loadCommittedTurn(ctx, tx, existing.ID); turnErr == nil {
			existing.CommittedTurn = &turn
		} else if !errors.Is(turnErr, ErrNotFound) {
			return Job{}, false, turnErr
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Job{}, false, err
	}

	var ownerID string
	var simulationVersion int64
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(user_id, 1), version FROM simulations WHERE id=UUID_TO_BIN(?, 1) AND status='active' FOR UPDATE`, input.SimulationID).Scan(&ownerID, &simulationVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, ErrNotFound
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("lock simulation: %w", err)
	}
	if ownerID != input.UserID {
		return Job{}, false, ErrForbidden
	}

	var available, reserved, walletVersion int64
	err = tx.QueryRowContext(ctx, `SELECT available_balance, reserved_balance, version FROM wallets WHERE user_id=UUID_TO_BIN(?, 1) FOR UPDATE`, input.UserID).Scan(&available, &reserved, &walletVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, ErrForbidden
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("lock wallet: %w", err)
	}
	var activeCount int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_jobs WHERE user_id=UUID_TO_BIN(?, 1) AND status IN ('queued','planning','drafting','validating','committing')`, input.UserID).Scan(&activeCount)
	if err != nil {
		return Job{}, false, fmt.Errorf("check active job: %w", err)
	}
	if activeCount > 0 {
		return Job{}, false, ErrActiveJobExists
	}
	if available < input.Credits {
		return Job{}, false, ErrInsufficientCredits
	}

	jobID, err := newUUID()
	if err != nil {
		return Job{}, false, err
	}
	entryID, err := newUUID()
	if err != nil {
		return Job{}, false, err
	}
	outboxID, err := newUUID()
	if err != nil {
		return Job{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_jobs (id,user_id,simulation_id,idempotency_key,user_action,interaction,status,reserved_credits,input_version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,'queued',?,?)`, jobID, input.UserID, input.SimulationID, input.IdempotencyKey, input.Action, input.Interaction, input.Credits, simulationVersion)
	if err != nil {
		return Job{}, false, translateMySQLError("insert job", err)
	}
	newAvailable, newReserved := available-input.Credits, reserved+input.Credits
	_, err = tx.ExecContext(ctx, `UPDATE wallets SET available_balance=?, reserved_balance=?, version=version+1 WHERE user_id=UUID_TO_BIN(?,1) AND version=?`, newAvailable, newReserved, input.UserID, walletVersion)
	if err != nil {
		return Job{}, false, fmt.Errorf("reserve wallet: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO wallet_entries (id,user_id,entry_type,available_delta,reserved_delta,available_after,reserved_after,biz_type,biz_id,idempotency_key) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'reserve',?,?,?,?, 'turn_job',UUID_TO_BIN(?,1),?)`, entryID, input.UserID, -input.Credits, input.Credits, newAvailable, newReserved, jobID, jobID+":reserve:1")
	if err != nil {
		return Job{}, false, fmt.Errorf("record reservation: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{"jobId": jobID, "simulationId": input.SimulationID})
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events (id,aggregate_type,aggregate_id,event_type,payload_json) VALUES (UUID_TO_BIN(?,1),'turn_job',UUID_TO_BIN(?,1),'turn_job.queued',?)`, outboxID, jobID, payload)
	if err != nil {
		return Job{}, false, fmt.Errorf("write job outbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Job{}, false, fmt.Errorf("commit create job: %w", err)
	}
	job, err := s.Get(ctx, input.UserID, jobID)
	return job, true, err
}

func (s *Store) Get(ctx context.Context, userID, jobID string) (Job, error) {
	job, err := s.GetForProcessing(ctx, jobID)
	if err != nil {
		return Job{}, err
	}
	if job.UserID != userID {
		return Job{}, ErrForbidden
	}
	turn, err := loadCommittedTurn(ctx, s.db, jobID)
	if err == nil {
		job.CommittedTurn = &turn
	} else if !errors.Is(err, ErrNotFound) {
		return Job{}, err
	}
	return job, nil
}

// SimulationSummary is a lightweight entry for the "my journeys" list. The
// world title is read from the frozen snapshot so archived worlds still render.
type SimulationSummary struct {
	ID          string    `json:"id"`
	WorldID     string    `json:"worldId"`
	WorldTitle  string    `json:"worldTitle"`
	Summary     string    `json:"summary"`
	Status      string    `json:"status"`
	LastTurnSeq int       `json:"lastTurnSeq"`
	LastSceneNo int       `json:"lastSceneNo"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (s *Store) ListSimulations(ctx context.Context, userID string) ([]SimulationSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(world_id,1),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(world_snapshot_json,'$.world.title')),''),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(world_snapshot_json,'$.world.summary')),summary),status,last_turn_seq,last_scene_no,updated_at FROM simulations WHERE user_id=UUID_TO_BIN(?,1) AND status='active' ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list simulations: %w", err)
	}
	defer rows.Close()
	summaries := make([]SimulationSummary, 0)
	for rows.Next() {
		var summary SimulationSummary
		if err := rows.Scan(&summary.ID, &summary.WorldID, &summary.WorldTitle, &summary.Summary, &summary.Status, &summary.LastTurnSeq, &summary.LastSceneNo, &summary.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan simulation summary: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate simulations: %w", err)
	}
	return summaries, nil
}

func (s *Store) GetForProcessing(ctx context.Context, jobID string) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(j.id,1),BIN_TO_UUID(j.user_id,1),BIN_TO_UUID(j.simulation_id,1),j.idempotency_key,j.user_action,j.interaction,j.status,j.reserved_credits,j.input_version,COALESCE(j.lease_owner,''),j.lease_expires_at,j.cancel_requested_at,COALESCE(j.error_code,''),j.created_at,j.updated_at,(SELECT COUNT(*) FROM ai_attempts a WHERE a.job_id=j.id) FROM ai_jobs j WHERE j.id=UUID_TO_BIN(?,1)`, jobID))
}

func (s *Store) ListQueuedIDs(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id,1) FROM ai_jobs WHERE status='queued' ORDER BY created_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list queued jobs: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) RecoverForRedelivery(ctx context.Context, jobID string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	job, err := getJobForUpdate(ctx, tx, jobID)
	if err != nil {
		return false, err
	}
	switch job.Status {
	case StatusPlanning, StatusDrafting, StatusValidating:
		_, err = tx.ExecContext(ctx, `UPDATE ai_attempts SET status='failed',error_code='worker_lost',finished_at=CURRENT_TIMESTAMP(6) WHERE job_id=UUID_TO_BIN(?,1) AND status='started'`, jobID)
		if err != nil {
			return false, fmt.Errorf("fail interrupted attempt: %w", err)
		}
		_, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status='queued',error_code='worker_lost',lease_owner=NULL,lease_expires_at=NULL WHERE id=UUID_TO_BIN(?,1)`, jobID)
		if err != nil {
			return false, fmt.Errorf("recover interrupted job: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func (s *Store) LoadSimulation(ctx context.Context, job Job) (storyengine.Simulation, error) {
	var sim storyengine.Simulation
	var worldSnapshot, state []byte
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(world_id,1),world_snapshot_json,state_json,summary,version,last_turn_seq,last_scene_no FROM simulations WHERE id=UUID_TO_BIN(?,1) AND user_id=UUID_TO_BIN(?,1)`, job.SimulationID, job.UserID).Scan(&sim.ID, &sim.WorldID, &worldSnapshot, &state, &sim.Summary, &sim.Version, &sim.LastSeq, &sim.LastSceneNo)
	if errors.Is(err, sql.ErrNoRows) {
		return sim, ErrNotFound
	}
	if err != nil {
		return sim, fmt.Errorf("load simulation: %w", err)
	}
	sim.WorldSnapshot = append(json.RawMessage(nil), worldSnapshot...)
	if err := json.Unmarshal(state, &sim.State); err != nil {
		return sim, fmt.Errorf("decode simulation state: %w", err)
	}
	sim.Characters = map[string]storyengine.CharacterState{}
	rows, err := s.db.QueryContext(ctx, `SELECT character_id,name,identity_json,current_state_json,version FROM character_states WHERE simulation_id=UUID_TO_BIN(?,1)`, job.SimulationID)
	if err != nil {
		return sim, fmt.Errorf("load character states: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var character storyengine.CharacterState
		var identity, current []byte
		if err := rows.Scan(&character.ID, &character.Name, &identity, &current, &character.Version); err != nil {
			return sim, fmt.Errorf("scan character state: %w", err)
		}
		if err := json.Unmarshal(identity, &character.Identity); err != nil {
			return sim, fmt.Errorf("decode character identity: %w", err)
		}
		if err := json.Unmarshal(current, &character.Current); err != nil {
			return sim, fmt.Errorf("decode character state: %w", err)
		}
		sim.Characters[character.ID] = character
	}
	if err := rows.Err(); err != nil {
		return sim, fmt.Errorf("iterate character states: %w", err)
	}
	sim.Threads = map[string]storyengine.StoryThread{}
	threadRows, err := s.db.QueryContext(ctx, `SELECT thread_key,title,status,priority FROM story_threads WHERE simulation_id=UUID_TO_BIN(?,1)`, job.SimulationID)
	if err != nil {
		return sim, fmt.Errorf("load story threads: %w", err)
	}
	defer threadRows.Close()
	for threadRows.Next() {
		var thread storyengine.StoryThread
		if err := threadRows.Scan(&thread.Key, &thread.Title, &thread.Status, &thread.Priority); err != nil {
			return sim, fmt.Errorf("scan story thread: %w", err)
		}
		sim.Threads[thread.Key] = thread
	}
	if err := threadRows.Err(); err != nil {
		return sim, err
	}
	if err := s.ensureInitialProjections(ctx, &sim); err != nil {
		return sim, fmt.Errorf("ensure initial projections: %w", err)
	}
	return sim, nil
}

func (s *Store) LoadContinuity(ctx context.Context, job Job) (storyengine.ContinuityContext, error) {
	sim, err := s.LoadSimulation(ctx, job)
	if err != nil {
		return storyengine.ContinuityContext{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(simulation_id,1),seq,scene_no,scene_title,scene_closure,interaction,user_action,proposal_json,status,created_at FROM turns WHERE simulation_id=UUID_TO_BIN(?,1) AND status='committed' ORDER BY seq DESC LIMIT ?`, job.SimulationID, storyengine.DefaultRecentTurnLimit)
	if err != nil {
		return storyengine.ContinuityContext{}, fmt.Errorf("load committed turns: %w", err)
	}
	defer rows.Close()
	turns := make([]storyengine.Turn, 0, storyengine.DefaultRecentTurnLimit)
	for rows.Next() {
		var turn storyengine.Turn
		var proposal []byte
		if err := rows.Scan(&turn.ID, &turn.SimulationID, &turn.Seq, &turn.SceneNo, &turn.SceneTitle, &turn.SceneClosure, &turn.Interaction, &turn.UserAction, &proposal, &turn.Status, &turn.CreatedAt); err != nil {
			return storyengine.ContinuityContext{}, fmt.Errorf("scan committed turn: %w", err)
		}
		if err := json.Unmarshal(proposal, &turn.Proposal); err != nil {
			return storyengine.ContinuityContext{}, fmt.Errorf("decode committed turn proposal: %w", err)
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return storyengine.ContinuityContext{}, fmt.Errorf("iterate committed turns: %w", err)
	}
	return storyengine.BuildContinuityContext(storyengine.ContinuitySources{
		WorldSnapshot: sim.WorldSnapshot, CommittedSimulation: sim,
		CommittedSummary: sim.Summary, CommittedTurns: turns,
	})
}

func (s *Store) GetSimulationSnapshot(ctx context.Context, userID, simulationID string) (SimulationSnapshot, error) {
	job := Job{UserID: userID, SimulationID: simulationID}
	sim, err := s.LoadSimulation(ctx, job)
	if err != nil {
		return SimulationSnapshot{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(simulation_id,1),seq,scene_no,scene_title,scene_closure,interaction,user_action,proposal_json,status,created_at FROM turns WHERE simulation_id=UUID_TO_BIN(?,1) AND status='committed' ORDER BY seq DESC LIMIT 50`, simulationID)
	if err != nil {
		return SimulationSnapshot{}, fmt.Errorf("load simulation history: %w", err)
	}
	defer rows.Close()
	turns := make([]storyengine.Turn, 0, 50)
	for rows.Next() {
		var turn storyengine.Turn
		var proposal []byte
		if err := rows.Scan(&turn.ID, &turn.SimulationID, &turn.Seq, &turn.SceneNo, &turn.SceneTitle, &turn.SceneClosure, &turn.Interaction, &turn.UserAction, &proposal, &turn.Status, &turn.CreatedAt); err != nil {
			return SimulationSnapshot{}, fmt.Errorf("scan simulation history: %w", err)
		}
		if err := json.Unmarshal(proposal, &turn.Proposal); err != nil {
			return SimulationSnapshot{}, fmt.Errorf("decode simulation history: %w", err)
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return SimulationSnapshot{}, fmt.Errorf("iterate simulation history: %w", err)
	}
	for left, right := 0, len(turns)-1; left < right; left, right = left+1, right-1 {
		turns[left], turns[right] = turns[right], turns[left]
	}
	return SimulationSnapshot{
		ID: sim.ID, WorldID: sim.WorldID, WorldSnapshot: sim.WorldSnapshot,
		State: sim.State, Summary: sim.Summary, Version: sim.Version, LastTurnSeq: sim.LastSeq,
		LastSceneNo: sim.LastSceneNo, Characters: sim.Characters, Threads: sim.Threads, RecentTurns: turns,
	}, nil
}

func (s *Store) BeginAttempt(ctx context.Context, job Job, provider, model, workerID string) (Attempt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	locked, err := getJobForUpdate(ctx, tx, job.ID)
	if err != nil {
		return Attempt{}, err
	}
	if locked.Status != StatusQueued {
		return Attempt{}, fmt.Errorf("begin attempt from %s: %w", locked.Status, ErrConcurrentState)
	}
	var attemptNo int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt_no),0)+1 FROM ai_attempts WHERE job_id=UUID_TO_BIN(?,1)`, job.ID).Scan(&attemptNo); err != nil {
		return Attempt{}, err
	}
	attemptID, err := newUUID()
	if err != nil {
		return Attempt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_attempts (id,job_id,attempt_no,provider,model,status) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,'started')`, attemptID, job.ID, attemptNo, provider, model)
	if err != nil {
		return Attempt{}, fmt.Errorf("insert attempt: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status='planning',error_code=NULL,lease_owner=?,lease_expires_at=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 3 MINUTE) WHERE id=UUID_TO_BIN(?,1)`, workerID, job.ID)
	if err != nil {
		return Attempt{}, fmt.Errorf("start planning: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Attempt{}, err
	}
	return Attempt{ID: attemptID, JobID: job.ID, Number: attemptNo, Provider: provider, Model: model, Status: "started", StartedAt: time.Now().UTC()}, nil
}

func (s *Store) MarkDrafting(ctx context.Context, jobID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE ai_jobs SET status='drafting' WHERE id=UUID_TO_BIN(?,1) AND status='planning'`, jobID)
	if err != nil {
		return fmt.Errorf("mark drafting: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrConcurrentState
	}
	return nil
}

func (s *Store) MarkValidating(ctx context.Context, jobID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE ai_jobs SET status='validating' WHERE id=UUID_TO_BIN(?,1) AND status='drafting'`, jobID)
	if err != nil {
		return fmt.Errorf("mark validating: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return ErrConcurrentState
	}
	return nil
}

func (s *Store) CommitSuccess(ctx context.Context, input CommitInput) (storyengine.Turn, error) {
	proposalJSON, err := json.Marshal(input.Proposal)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("encode proposal: %w", err)
	}
	choicesJSON, err := json.Marshal(input.Proposal.Choices)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("encode choices: %w", err)
	}
	usageJSON, err := json.Marshal(input.Usage)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("encode usage: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return storyengine.Turn{}, err
	}
	defer tx.Rollback()
	job, err := getJobForUpdate(ctx, tx, input.Job.ID)
	if err != nil {
		return storyengine.Turn{}, err
	}
	if job.Status == StatusCancelled {
		return storyengine.Turn{}, ErrCannotCancel
	}
	if job.Status != StatusValidating {
		return storyengine.Turn{}, ErrConcurrentState
	}
	_, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status='committing' WHERE id=UUID_TO_BIN(?,1)`, job.ID)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("enter commit boundary: %w", err)
	}
	var simulationVersion int64
	var lastSeq int
	var lastSceneNo int
	err = tx.QueryRowContext(ctx, `SELECT version,last_turn_seq,last_scene_no FROM simulations WHERE id=UUID_TO_BIN(?,1) AND user_id=UUID_TO_BIN(?,1) FOR UPDATE`, job.SimulationID, job.UserID).Scan(&simulationVersion, &lastSeq, &lastSceneNo)
	if errors.Is(err, sql.ErrNoRows) {
		return storyengine.Turn{}, ErrNotFound
	}
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("lock simulation for commit: %w", err)
	}
	if simulationVersion != job.InputVersion || input.Proposal.InputVersion != simulationVersion || input.Proposal.ExpectedNextSeq != lastSeq+1 {
		return storyengine.Turn{}, ErrConcurrentState
	}
	sceneNo := lastSceneNo
	sceneTitle := ""
	sceneClosure := ""
	if input.Proposal.Scene.StartsNewScene {
		sceneNo = lastSceneNo + 1
		sceneTitle = strings.TrimSpace(input.Proposal.Scene.Title)
		sceneClosure = strings.TrimSpace(input.Proposal.Scene.Closure)
	}

	turnID, err := newUUID()
	if err != nil {
		return storyengine.Turn{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO turns (id,simulation_id,job_id,seq,scene_no,scene_title,scene_closure,interaction,user_action,proposal_json,narrative,choices_json) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,?,?,?,?,?,?)`, turnID, job.SimulationID, job.ID, lastSeq+1, sceneNo, sceneTitle, sceneClosure, job.Interaction, job.Action, proposalJSON, input.Proposal.Narrative, choicesJSON)
	if err != nil {
		return storyengine.Turn{}, translateMySQLError("insert turn", err)
	}
	for index, event := range input.Proposal.Events {
		payload, err := json.Marshal(event.Payload)
		if err != nil {
			return storyengine.Turn{}, fmt.Errorf("encode event %d: %w", index, err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO story_events (simulation_id,turn_id,seq,event_type,subject_ref,object_ref,payload_json) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,?,?)`, job.SimulationID, turnID, index+1, event.Type, event.SubjectRef, nullableString(event.ObjectRef), payload)
		if err != nil {
			return storyengine.Turn{}, fmt.Errorf("insert story event %d: %w", index, err)
		}
	}
	for _, patch := range input.Proposal.CharacterPatches {
		current, err := json.Marshal(patch.Current)
		if err != nil {
			return storyengine.Turn{}, fmt.Errorf("encode character patch: %w", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE character_states SET current_state_json=JSON_MERGE_PATCH(current_state_json,?),version=version+1,source_turn_id=UUID_TO_BIN(?,1) WHERE simulation_id=UUID_TO_BIN(?,1) AND character_id=? AND version=?`, current, turnID, job.SimulationID, patch.CharacterID, patch.ExpectedVersion)
		if err != nil {
			return storyengine.Turn{}, fmt.Errorf("apply character patch: %w", err)
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return storyengine.Turn{}, ErrConcurrentState
		}
	}
	for _, patch := range input.Proposal.ThreadPatches {
		switch patch.Action {
		case "open":
			_, err = tx.ExecContext(ctx, `INSERT INTO story_threads (simulation_id,thread_key,title,status,priority,opened_turn_id) VALUES (UUID_TO_BIN(?,1),?,?,'open',?,UUID_TO_BIN(?,1))`, job.SimulationID, patch.Key, patch.Title, patch.Priority, turnID)
		case "resolve":
			_, err = tx.ExecContext(ctx, `UPDATE story_threads SET status='resolved',resolved_turn_id=UUID_TO_BIN(?,1) WHERE simulation_id=UUID_TO_BIN(?,1) AND thread_key=? AND status='open'`, turnID, job.SimulationID, patch.Key)
		}
		if err != nil {
			return storyengine.Turn{}, fmt.Errorf("apply thread patch: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE simulations SET version=version+1,last_turn_seq=?,last_scene_no=?,updated_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1)`, lastSeq+1, sceneNo, job.SimulationID)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("advance simulation: %w", err)
	}

	var available, reserved, walletVersion int64
	err = tx.QueryRowContext(ctx, `SELECT available_balance,reserved_balance,version FROM wallets WHERE user_id=UUID_TO_BIN(?,1) FOR UPDATE`, job.UserID).Scan(&available, &reserved, &walletVersion)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("lock wallet for settlement: %w", err)
	}
	if reserved < job.ReservedCredits {
		return storyengine.Turn{}, ErrConcurrentState
	}
	// Dynamic cost by narrative length (floor of runes/100), settled against the
	// reserved max; the unused portion is refunded to the available balance.
	cost := TurnCost(len([]rune(input.Proposal.Narrative)))
	refund := int64(job.ReservedCredits) - int64(cost)
	if refund < 0 {
		refund = 0
	}
	newAvailable := available + refund
	newReserved := reserved - job.ReservedCredits
	_, err = tx.ExecContext(ctx, `UPDATE wallets SET available_balance=?,reserved_balance=?,version=version+1 WHERE user_id=UUID_TO_BIN(?,1) AND version=?`, newAvailable, newReserved, job.UserID, walletVersion)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("settle wallet: %w", err)
	}
	entryID, err := newUUID()
	if err != nil {
		return storyengine.Turn{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO wallet_entries (id,user_id,entry_type,available_delta,reserved_delta,available_after,reserved_after,biz_type,biz_id,idempotency_key) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'settle',?,?,?,?,?,UUID_TO_BIN(?,1),?)`, entryID, job.UserID, refund, -job.ReservedCredits, newAvailable, newReserved, "turn_job", job.ID, job.ID+":settle:"+fmt.Sprint(cost))
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("record settlement: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE ai_attempts SET status='completed',usage_json=?,finished_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1) AND job_id=UUID_TO_BIN(?,1) AND status='started'`, usageJSON, input.Attempt.ID, job.ID)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("complete attempt: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status='completed',lease_owner=NULL,lease_expires_at=NULL WHERE id=UUID_TO_BIN(?,1)`, job.ID)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("complete job: %w", err)
	}
	outboxID, err := newUUID()
	if err != nil {
		return storyengine.Turn{}, err
	}
	outboxPayload, _ := json.Marshal(map[string]any{"jobId": job.ID, "turnId": turnID, "seq": lastSeq + 1})
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events (id,aggregate_type,aggregate_id,event_type,payload_json) VALUES (UUID_TO_BIN(?,1),'turn_job',UUID_TO_BIN(?,1),'turn.committed',?)`, outboxID, job.ID, outboxPayload)
	if err != nil {
		return storyengine.Turn{}, fmt.Errorf("write turn outbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return storyengine.Turn{}, fmt.Errorf("commit turn: %w", err)
	}
	return storyengine.Turn{ID: turnID, SimulationID: job.SimulationID, Seq: lastSeq + 1, SceneNo: sceneNo, SceneTitle: sceneTitle, SceneClosure: sceneClosure, Interaction: job.Interaction, UserAction: job.Action, Proposal: input.Proposal, CreatedAt: time.Now().UTC()}, nil
}

func (s *Store) Fail(ctx context.Context, jobID, attemptID, errorCode string) error {
	return s.finishWithoutTurn(ctx, jobID, attemptID, StatusFailed, errorCode)
}

func (s *Store) Cancel(ctx context.Context, userID, jobID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := getJobForUpdate(ctx, tx, jobID)
	if err != nil {
		return err
	}
	if job.UserID != userID {
		return ErrForbidden
	}
	if job.Status == StatusCompleted || job.Status == StatusCommitting {
		return ErrCannotCancel
	}
	if job.Status == StatusCancelled {
		return nil
	}
	if job.Status == StatusFailed {
		return ErrCannotCancel
	}
	return finishWithoutTurnTx(ctx, tx, job, "", StatusCancelled, "cancelled_by_user")
}

func (s *Store) Retry(ctx context.Context, userID, jobID string) (Job, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	job, err := getJobForUpdate(ctx, tx, jobID)
	if err != nil {
		return Job{}, err
	}
	if job.UserID != userID {
		return Job{}, ErrForbidden
	}
	if job.Status != StatusFailed {
		return Job{}, ErrCannotRetry
	}
	var simulationVersion int64
	err = tx.QueryRowContext(ctx, `SELECT version FROM simulations WHERE id=UUID_TO_BIN(?,1) AND user_id=UUID_TO_BIN(?,1) FOR UPDATE`, job.SimulationID, userID).Scan(&simulationVersion)
	if err != nil {
		return Job{}, fmt.Errorf("lock simulation for retry: %w", err)
	}
	var available, reserved, walletVersion int64
	err = tx.QueryRowContext(ctx, `SELECT available_balance,reserved_balance,version FROM wallets WHERE user_id=UUID_TO_BIN(?,1) FOR UPDATE`, userID).Scan(&available, &reserved, &walletVersion)
	if err != nil {
		return Job{}, fmt.Errorf("lock wallet for retry: %w", err)
	}
	var activeCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_jobs WHERE user_id=UUID_TO_BIN(?,1) AND id<>UUID_TO_BIN(?,1) AND status IN ('queued','planning','drafting','validating','committing')`, userID, jobID).Scan(&activeCount); err != nil {
		return Job{}, err
	}
	if activeCount > 0 {
		return Job{}, ErrActiveJobExists
	}
	if available < job.ReservedCredits {
		return Job{}, ErrInsufficientCredits
	}
	newAvailable, newReserved := available-job.ReservedCredits, reserved+job.ReservedCredits
	_, err = tx.ExecContext(ctx, `UPDATE wallets SET available_balance=?,reserved_balance=?,version=version+1 WHERE user_id=UUID_TO_BIN(?,1) AND version=?`, newAvailable, newReserved, userID, walletVersion)
	if err != nil {
		return Job{}, fmt.Errorf("reserve retry credits: %w", err)
	}
	entryID, err := newUUID()
	if err != nil {
		return Job{}, err
	}
	reserveKey := fmt.Sprintf("%s:reserve:%d", job.ID, job.AttemptNo+1)
	_, err = tx.ExecContext(ctx, `INSERT INTO wallet_entries (id,user_id,entry_type,available_delta,reserved_delta,available_after,reserved_after,biz_type,biz_id,idempotency_key) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'reserve',?,?,?,?, 'turn_job',UUID_TO_BIN(?,1),?)`, entryID, userID, -job.ReservedCredits, job.ReservedCredits, newAvailable, newReserved, job.ID, reserveKey)
	if err != nil {
		return Job{}, fmt.Errorf("record retry reservation: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status='queued',input_version=?,error_code=NULL,cancel_requested_at=NULL,lease_owner=NULL,lease_expires_at=NULL WHERE id=UUID_TO_BIN(?,1)`, simulationVersion, job.ID)
	if err != nil {
		return Job{}, fmt.Errorf("queue retry: %w", err)
	}
	outboxID, err := newUUID()
	if err != nil {
		return Job{}, err
	}
	payload, _ := json.Marshal(map[string]any{"jobId": job.ID, "attemptNo": job.AttemptNo + 1})
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events (id,aggregate_type,aggregate_id,event_type,payload_json) VALUES (UUID_TO_BIN(?,1),'turn_job',UUID_TO_BIN(?,1),'turn_job.queued',?)`, outboxID, job.ID, payload)
	if err != nil {
		return Job{}, fmt.Errorf("write retry outbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return s.Get(ctx, userID, jobID)
}

func (s *Store) finishWithoutTurn(ctx context.Context, jobID, attemptID string, status Status, errorCode string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := getJobForUpdate(ctx, tx, jobID)
	if err != nil {
		return err
	}
	if job.Status == StatusCompleted || job.Status == StatusCancelled {
		return nil
	}
	if job.Status == StatusCommitting {
		return ErrConcurrentState
	}
	return finishWithoutTurnTx(ctx, tx, job, attemptID, status, errorCode)
}

func finishWithoutTurnTx(ctx context.Context, tx *sql.Tx, job Job, attemptID string, status Status, errorCode string) error {
	var available, reserved, walletVersion int64
	err := tx.QueryRowContext(ctx, `SELECT available_balance,reserved_balance,version FROM wallets WHERE user_id=UUID_TO_BIN(?,1) FOR UPDATE`, job.UserID).Scan(&available, &reserved, &walletVersion)
	if err != nil {
		return fmt.Errorf("lock wallet for release: %w", err)
	}
	if reserved < job.ReservedCredits {
		return ErrConcurrentState
	}
	newAvailable, newReserved := available+job.ReservedCredits, reserved-job.ReservedCredits
	_, err = tx.ExecContext(ctx, `UPDATE wallets SET available_balance=?,reserved_balance=?,version=version+1 WHERE user_id=UUID_TO_BIN(?,1) AND version=?`, newAvailable, newReserved, job.UserID, walletVersion)
	if err != nil {
		return fmt.Errorf("release wallet: %w", err)
	}
	entryID, err := newUUID()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO wallet_entries (id,user_id,entry_type,available_delta,reserved_delta,available_after,reserved_after,biz_type,biz_id,idempotency_key) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'release',?,?,?,?, 'turn_job',UUID_TO_BIN(?,1),?)`, entryID, job.UserID, job.ReservedCredits, -job.ReservedCredits, newAvailable, newReserved, job.ID, job.ID+":release:"+fmt.Sprint(job.AttemptNo+1))
	if err != nil {
		return fmt.Errorf("record release: %w", err)
	}
	if attemptID != "" {
		_, err = tx.ExecContext(ctx, `UPDATE ai_attempts SET status=?,error_code=?,finished_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1) AND job_id=UUID_TO_BIN(?,1) AND status='started'`, map[Status]string{StatusFailed: "failed", StatusCancelled: "cancelled"}[status], errorCode, attemptID, job.ID)
		if err != nil {
			return fmt.Errorf("finish attempt: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status=?,error_code=?,cancel_requested_at=IF(?='cancelled',CURRENT_TIMESTAMP(6),cancel_requested_at),lease_owner=NULL,lease_expires_at=NULL WHERE id=UUID_TO_BIN(?,1)`, status, errorCode, status, job.ID)
	if err != nil {
		return fmt.Errorf("finish job: %w", err)
	}
	outboxID, err := newUUID()
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"jobId": job.ID, "status": status, "errorCode": errorCode})
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events (id,aggregate_type,aggregate_id,event_type,payload_json) VALUES (UUID_TO_BIN(?,1),'turn_job',UUID_TO_BIN(?,1),?,?)`, outboxID, job.ID, "turn_job."+string(status), payload)
	if err != nil {
		return fmt.Errorf("write terminal outbox: %w", err)
	}
	return tx.Commit()
}

func getJobByIdempotency(ctx context.Context, tx *sql.Tx, userID, key string) (Job, error) {
	return scanJob(tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(j.id,1),BIN_TO_UUID(j.user_id,1),BIN_TO_UUID(j.simulation_id,1),j.idempotency_key,j.user_action,j.interaction,j.status,j.reserved_credits,j.input_version,COALESCE(j.lease_owner,''),j.lease_expires_at,j.cancel_requested_at,COALESCE(j.error_code,''),j.created_at,j.updated_at,(SELECT COUNT(*) FROM ai_attempts a WHERE a.job_id=j.id) FROM ai_jobs j WHERE j.user_id=UUID_TO_BIN(?,1) AND j.idempotency_key=?`, userID, key))
}

func getJobForUpdate(ctx context.Context, tx *sql.Tx, jobID string) (Job, error) {
	return scanJob(tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(j.id,1),BIN_TO_UUID(j.user_id,1),BIN_TO_UUID(j.simulation_id,1),j.idempotency_key,j.user_action,j.interaction,j.status,j.reserved_credits,j.input_version,COALESCE(j.lease_owner,''),j.lease_expires_at,j.cancel_requested_at,COALESCE(j.error_code,''),j.created_at,j.updated_at,(SELECT COUNT(*) FROM ai_attempts a WHERE a.job_id=j.id) FROM ai_jobs j WHERE j.id=UUID_TO_BIN(?,1) FOR UPDATE`, jobID))
}

type rowScanner interface{ Scan(dest ...any) error }

func scanJob(row rowScanner) (Job, error) {
	var job Job
	err := row.Scan(&job.ID, &job.UserID, &job.SimulationID, &job.IdempotencyKey, &job.Action, &job.Interaction, &job.Status, &job.ReservedCredits, &job.InputVersion, &job.LeaseOwner, &job.LeaseExpiresAt, &job.CancelRequestedAt, &job.ErrorCode, &job.CreatedAt, &job.UpdatedAt, &job.AttemptNo)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("scan job: %w", err)
	}
	return job, nil
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func loadCommittedTurn(ctx context.Context, q queryRower, jobID string) (storyengine.Turn, error) {
	var turn storyengine.Turn
	var proposal []byte
	err := q.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(simulation_id,1),seq,scene_no,scene_title,scene_closure,interaction,user_action,proposal_json,created_at FROM turns WHERE job_id=UUID_TO_BIN(?,1)`, jobID).Scan(&turn.ID, &turn.SimulationID, &turn.Seq, &turn.SceneNo, &turn.SceneTitle, &turn.SceneClosure, &turn.Interaction, &turn.UserAction, &proposal, &turn.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return turn, ErrNotFound
	}
	if err != nil {
		return turn, fmt.Errorf("load committed turn: %w", err)
	}
	if err := json.Unmarshal(proposal, &turn.Proposal); err != nil {
		return turn, fmt.Errorf("decode proposal: %w", err)
	}
	return turn, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func translateMySQLError(operation string, err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return fmt.Errorf("%s: %w", operation, ErrConcurrentState)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
