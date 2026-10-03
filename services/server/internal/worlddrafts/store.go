package worlddrafts

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, input CreateInput) (Job, bool, error) {
	if len(input.IdempotencyKey) < 16 || len(input.IdempotencyKey) > 128 { return Job{}, false, fmt.Errorf("invalid idempotency key") }
	normalized, inputHash, err := NormalizeAndHashInput(input.GenerateInput)
	if err != nil { return Job{}, false, err }
	payload, err := json.Marshal(normalized)
	if err != nil { return Job{}, false, fmt.Errorf("encode world draft input: %w", err) }
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil { return Job{}, false, fmt.Errorf("begin world draft job: %w", err) }
	defer tx.Rollback()
	existing, err := getJobByIdempotency(ctx, tx, input.UserID, input.IdempotencyKey, true)
	if err == nil {
		if subtle.ConstantTimeCompare(existing.InputHash[:], inputHash[:]) != 1 { return Job{}, false, ErrIdempotencyConflict }
		return existing.withPolicy(), false, nil
	}
	if !errors.Is(err, ErrNotFound) { return Job{}, false, err }
	var userStatus string
	err = tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id=UUID_TO_BIN(?,1) FOR UPDATE`, input.UserID).Scan(&userStatus)
	if errors.Is(err, sql.ErrNoRows) { return Job{}, false, ErrForbidden }
	if err != nil { return Job{}, false, fmt.Errorf("lock world draft owner: %w", err) }
	if userStatus != "active" { return Job{}, false, ErrForbidden }
	jobID, err := newUUID(); if err != nil { return Job{}, false, err }
	outboxID, err := newUUID(); if err != nil { return Job{}, false, err }
	_, err = tx.ExecContext(ctx, `INSERT INTO world_draft_jobs (id,user_id,idempotency_key,input_json,input_hash,status,max_attempts,available_at) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,'queued',3,CURRENT_TIMESTAMP(6))`, jobID,input.UserID,input.IdempotencyKey,payload,inputHash[:])
	if err != nil {
		if isDuplicate(err) {
			_ = tx.Rollback()
			existing, loadErr := s.getByIdempotency(ctx,input.UserID,input.IdempotencyKey)
			if loadErr != nil { return Job{}, false, loadErr }
			if subtle.ConstantTimeCompare(existing.InputHash[:],inputHash[:]) != 1 { return Job{}, false, ErrIdempotencyConflict }
			return existing.withPolicy(),false,nil
		}
		return Job{},false,fmt.Errorf("insert world draft job: %w",err)
	}
	outboxPayload,_ := json.Marshal(map[string]any{"jobId":jobID})
	_,err=tx.ExecContext(ctx,`INSERT INTO outbox_events (id,aggregate_type,aggregate_id,event_type,payload_json) VALUES (UUID_TO_BIN(?,1),'world_draft_job',UUID_TO_BIN(?,1),'world_draft.queued',?)`,outboxID,jobID,outboxPayload)
	if err != nil { return Job{},false,fmt.Errorf("write world draft outbox: %w",err) }
	if err:=tx.Commit(); err!=nil { return Job{},false,fmt.Errorf("commit world draft job: %w",err) }
	job,err:=s.GetForProcessing(ctx,jobID)
	return job.withPolicy(),true,err
}

func (s *Store) Get(ctx context.Context,userID,jobID string)(Job,error){
	job,err:=s.GetForProcessing(ctx,jobID); if err!=nil{return Job{},err}
	if job.UserID!=userID{return Job{},ErrForbidden}
	if job.Status==StatusCompleted&&job.WorldID!=""{result,err:=loadWorld(ctx,s.db,job.UserID,job.WorldID);if err!=nil{return Job{},err};job.Result=&result}
	return job.withPolicy(),nil
}

func(s *Store)GetForProcessing(ctx context.Context,jobID string)(Job,error){return scanJob(s.db.QueryRowContext(ctx,jobSelect+` WHERE j.id=UUID_TO_BIN(?,1)`,jobID))}
func(s *Store)getByIdempotency(ctx context.Context,userID,key string)(Job,error){return scanJob(s.db.QueryRowContext(ctx,jobSelect+` WHERE j.user_id=UUID_TO_BIN(?,1) AND j.idempotency_key=?`,userID,key))}

func(s *Store)ListProcessableIDs(ctx context.Context,limit int)([]string,error){
	if limit<1||limit>100{limit=10}
	rows,err:=s.db.QueryContext(ctx,`SELECT BIN_TO_UUID(id,1) FROM world_draft_jobs WHERE (status='queued' AND available_at<=CURRENT_TIMESTAMP(6)) OR (status IN ('generating','validating') AND lease_expires_at<CURRENT_TIMESTAMP(6)) ORDER BY available_at,created_at LIMIT ?`,limit)
	if err!=nil{return nil,fmt.Errorf("list processable world draft jobs: %w",err)}
	defer rows.Close();ids:=make([]string,0,limit)
	for rows.Next(){var id string;if err:=rows.Scan(&id);err!=nil{return nil,err};ids=append(ids,id)}
	return ids,rows.Err()
}

func(s *Store)BeginAttempt(ctx context.Context,jobID,provider,model,workerID string)(Attempt,error){
	if provider==""||model==""||workerID==""{return Attempt{},ErrInvalidInput}
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Attempt{},err};defer tx.Rollback()
	job,err:=getJobForUpdate(ctx,tx,jobID);if err!=nil{return Attempt{},err}
	if job.Status!=StatusQueued||job.AvailableAt.After(time.Now().UTC()){return Attempt{},ErrConcurrentState}
	if job.AttemptNo>=job.MaxAttempts{return Attempt{},ErrAttemptsExhausted}
	attemptNo:=job.AttemptNo+1;attemptID,err:=newUUID();if err!=nil{return Attempt{},err}
	_,err=tx.ExecContext(ctx,`INSERT INTO world_draft_attempts (id,job_id,attempt_no,provider,model,status) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,'started')`,attemptID,jobID,attemptNo,provider,model)
	if err!=nil{return Attempt{},fmt.Errorf("insert world draft attempt: %w",err)}
	result,err:=tx.ExecContext(ctx,`UPDATE world_draft_jobs SET status='generating',attempt_count=?,error_code=NULL,lease_owner=?,lease_expires_at=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 3 MINUTE) WHERE id=UUID_TO_BIN(?,1) AND status='queued'`,attemptNo,workerID,jobID)
	if err!=nil{return Attempt{},fmt.Errorf("start world draft attempt: %w",err)}
	if count,_:=result.RowsAffected();count!=1{return Attempt{},ErrConcurrentState}
	if err:=tx.Commit();err!=nil{return Attempt{},err}
	return Attempt{ID:attemptID,JobID:jobID,Number:attemptNo,Provider:provider,Model:model,Status:"started",StartedAt:time.Now().UTC()},nil
}

func(s *Store)MarkValidating(ctx context.Context,jobID,workerID string)error{
	result,err:=s.db.ExecContext(ctx,`UPDATE world_draft_jobs SET status='validating',lease_expires_at=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 3 MINUTE) WHERE id=UUID_TO_BIN(?,1) AND status='generating' AND lease_owner=? AND lease_expires_at>CURRENT_TIMESTAMP(6)`,jobID,workerID)
	if err!=nil{return fmt.Errorf("mark world draft validating: %w",err)}
	if count,_:=result.RowsAffected();count!=1{return ErrConcurrentState};return nil
}

func(s *Store)CommitSuccess(ctx context.Context,input CommitInput)(WorldResult,error){
	if err:=input.Draft.Validate();err!=nil{return WorldResult{},err}
	setting,err:=input.Draft.SettingJSON();if err!=nil{return WorldResult{},fmt.Errorf("encode generated world setting: %w",err)}
	usageJSON,err:=json.Marshal(input.Usage);if err!=nil{return WorldResult{},fmt.Errorf("encode world draft usage: %w",err)}
	tx,err:=s.db.BeginTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted});if err!=nil{return WorldResult{},err};defer tx.Rollback()
	job,err:=getJobForUpdate(ctx,tx,input.Job.ID);if err!=nil{return WorldResult{},err}
	if job.Status==StatusCompleted&&job.WorldID!=""{return loadWorld(ctx,tx,job.UserID,job.WorldID)}
	if job.Status!=StatusValidating||job.LeaseOwner!=input.WorkerID||job.LeaseExpiresAt==nil||job.LeaseExpiresAt.Before(time.Now().UTC())||job.AttemptNo!=input.Attempt.Number{return WorldResult{},ErrConcurrentState}
	result,err:=tx.ExecContext(ctx,`UPDATE world_draft_jobs SET status='committing' WHERE id=UUID_TO_BIN(?,1) AND status='validating' AND lease_owner=?`,job.ID,input.WorkerID)
	if err!=nil{return WorldResult{},fmt.Errorf("enter world draft commit boundary: %w",err)};if count,_:=result.RowsAffected();count!=1{return WorldResult{},ErrConcurrentState}
	worldID,err:=newUUID();if err!=nil{return WorldResult{},err}
	_,err=tx.ExecContext(ctx,`INSERT INTO worlds (id,owner_id,title,summary,setting_json,draft_source,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,?,'ai','private','draft',1)`,worldID,job.UserID,input.Draft.Title,input.Draft.Summary,setting)
	if err!=nil{return WorldResult{},fmt.Errorf("insert generated private world: %w",err)}
	result,err=tx.ExecContext(ctx,`UPDATE world_draft_attempts SET status='completed',usage_json=?,finished_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1) AND job_id=UUID_TO_BIN(?,1) AND status='started'`,usageJSON,input.Attempt.ID,job.ID)
	if err!=nil{return WorldResult{},fmt.Errorf("complete world draft attempt: %w",err)};if count,_:=result.RowsAffected();count!=1{return WorldResult{},ErrConcurrentState}
	result,err=tx.ExecContext(ctx,`UPDATE world_draft_jobs SET status='completed',world_id=UUID_TO_BIN(?,1),lease_owner=NULL,lease_expires_at=NULL,completed_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1) AND status='committing'`,worldID,job.ID)
	if err!=nil{return WorldResult{},fmt.Errorf("complete world draft job: %w",err)};if count,_:=result.RowsAffected();count!=1{return WorldResult{},ErrConcurrentState}
	outboxID,err:=newUUID();if err!=nil{return WorldResult{},err};outboxPayload,_:=json.Marshal(map[string]any{"jobId":job.ID,"worldId":worldID})
	_,err=tx.ExecContext(ctx,`INSERT INTO outbox_events (id,aggregate_type,aggregate_id,event_type,payload_json) VALUES (UUID_TO_BIN(?,1),'world_draft_job',UUID_TO_BIN(?,1),'world_draft.completed',?)`,outboxID,job.ID,outboxPayload)
	if err!=nil{return WorldResult{},fmt.Errorf("write world draft completion outbox: %w",err)}
	world,err:=loadWorld(ctx,tx,job.UserID,worldID);if err!=nil{return WorldResult{},err}
	if err:=tx.Commit();err!=nil{return WorldResult{},fmt.Errorf("commit generated world: %w",err)};return world,nil
}

func(s *Store)Fail(ctx context.Context,jobID,attemptID,workerID,errorCode string)error{
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback();job,err:=getJobForUpdate(ctx,tx,jobID);if err!=nil{return err}
	if job.Status==StatusFailed||job.Status==StatusCancelled||job.Status==StatusCompleted{return nil}
	if job.Status==StatusCommitting||job.LeaseOwner!=workerID{return ErrConcurrentState}
	if attemptID!=""{_,err=tx.ExecContext(ctx,`UPDATE world_draft_attempts SET status='failed',error_code=?,finished_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1) AND job_id=UUID_TO_BIN(?,1) AND status='started'`,errorCode,attemptID,jobID);if err!=nil{return fmt.Errorf("fail world draft attempt: %w",err)}}
	_,err=tx.ExecContext(ctx,`UPDATE world_draft_jobs SET status='failed',error_code=?,lease_owner=NULL,lease_expires_at=NULL,completed_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1)`,errorCode,jobID);if err!=nil{return fmt.Errorf("fail world draft job: %w",err)};return tx.Commit()
}

func(s *Store)RecoverExpired(ctx context.Context,jobID string)(bool,error){
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return false,err};defer tx.Rollback();job,err:=getJobForUpdate(ctx,tx,jobID);if err!=nil{return false,err}
	if job.Status!=StatusGenerating&&job.Status!=StatusValidating{return false,nil};if job.LeaseExpiresAt==nil||job.LeaseExpiresAt.After(time.Now().UTC()){return false,nil}
	_,err=tx.ExecContext(ctx,`UPDATE world_draft_attempts SET status='failed',error_code='worker_lost',finished_at=CURRENT_TIMESTAMP(6) WHERE job_id=UUID_TO_BIN(?,1) AND status='started'`,jobID);if err!=nil{return false,fmt.Errorf("fail expired world draft attempt: %w",err)}
	if job.AttemptNo>=job.MaxAttempts{_,err=tx.ExecContext(ctx,`UPDATE world_draft_jobs SET status='failed',error_code='attempts_exhausted',lease_owner=NULL,lease_expires_at=NULL,completed_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1)`,jobID)}else{_,err=tx.ExecContext(ctx,`UPDATE world_draft_jobs SET status='queued',error_code='worker_lost',lease_owner=NULL,lease_expires_at=NULL,available_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1)`,jobID);if err==nil{outboxID,idErr:=newUUID();if idErr!=nil{return false,idErr};payload,_:=json.Marshal(map[string]any{"jobId":jobID,"recovered":true});_,err=tx.ExecContext(ctx,`INSERT INTO outbox_events (id,aggregate_type,aggregate_id,event_type,payload_json) VALUES (UUID_TO_BIN(?,1),'world_draft_job',UUID_TO_BIN(?,1),'world_draft.queued',?)`,outboxID,jobID,payload)}}
	if err!=nil{return false,fmt.Errorf("recover expired world draft job: %w",err)};if err:=tx.Commit();err!=nil{return false,err};return job.AttemptNo<job.MaxAttempts,nil
}

func (s *Store) Cancel(ctx context.Context, userID, jobID string) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil { return Job{}, err }
	defer tx.Rollback()
	job, err := getJobForUpdate(ctx, tx, jobID)
	if err != nil { return Job{}, err }
	if job.UserID != userID { return Job{}, ErrForbidden }
	switch job.Status {
	case StatusCompleted, StatusCommitting, StatusFailed, StatusCancelled:
		if job.Status == StatusCancelled { return s.Get(ctx, userID, jobID) }
		return Job{}, ErrConcurrentState
	}
	outboxID, err := newUUID(); if err != nil { return Job{}, err }
	payload, _ := json.Marshal(map[string]any{"jobId": job.ID, "status": "cancelled", "errorCode": "cancelled_by_user"})
	result, err := tx.ExecContext(ctx, `UPDATE world_draft_jobs SET status='cancelled',error_code='cancelled_by_user',lease_owner=NULL,lease_expires_at=NULL,completed_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1) AND status IN ('queued','generating','validating')`, job.ID)
	if err != nil { return Job{}, fmt.Errorf("cancel world draft job: %w", err) }
	if count, _ := result.RowsAffected(); count != 1 { return Job{}, ErrConcurrentState }
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_events (id,aggregate_type,aggregate_id,event_type,payload_json) VALUES (UUID_TO_BIN(?,1),'world_draft_job',UUID_TO_BIN(?,1),'world_draft.cancelled',?)`, outboxID, job.ID, payload); err != nil {
		return Job{}, fmt.Errorf("write world draft cancellation outbox: %w", err)
	}
	if err := tx.Commit(); err != nil { return Job{}, err }
	return s.Get(ctx, userID, jobID)
}

func (s *Store) Retry(ctx context.Context, userID, jobID string) (Job, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil { return Job{}, err }
	defer tx.Rollback()
	job, err := getJobForUpdate(ctx, tx, jobID)
	if err != nil { return Job{}, err }
	if job.UserID != userID { return Job{}, ErrForbidden }
	if job.Status != StatusFailed { return Job{}, ErrConcurrentState }
	outboxID, err := newUUID(); if err != nil { return Job{}, err }
	payload, _ := json.Marshal(map[string]any{"jobId": job.ID, "attemptNo": job.AttemptNo + 1})
	_, err = tx.ExecContext(ctx, `UPDATE world_draft_jobs SET status='queued',error_code=NULL,lease_owner=NULL,lease_expires_at=NULL,available_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1)`, job.ID)
	if err != nil { return Job{}, fmt.Errorf("queue world draft retry: %w", err) }
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_events (id,aggregate_type,aggregate_id,event_type,payload_json) VALUES (UUID_TO_BIN(?,1),'world_draft_job',UUID_TO_BIN(?,1),'world_draft.queued',?)`, outboxID, job.ID, payload); err != nil {
		return Job{}, fmt.Errorf("write world draft retry outbox: %w", err)
	}
	if err := tx.Commit(); err != nil { return Job{}, err }
	return s.Get(ctx, userID, jobID)
}

const jobSelect=`SELECT BIN_TO_UUID(j.id,1),BIN_TO_UUID(j.user_id,1),j.idempotency_key,j.input_json,j.input_hash,j.status,j.attempt_count,j.max_attempts,j.available_at,COALESCE(j.lease_owner,''),j.lease_expires_at,COALESCE(BIN_TO_UUID(j.world_id,1),''),COALESCE(j.error_code,''),j.created_at,j.updated_at FROM world_draft_jobs j`
func getJobByIdempotency(ctx context.Context,tx *sql.Tx,userID,key string,forUpdate bool)(Job,error){query:=jobSelect+` WHERE j.user_id=UUID_TO_BIN(?,1) AND j.idempotency_key=?`;if forUpdate{query+=` FOR UPDATE`};return scanJob(tx.QueryRowContext(ctx,query,userID,key))}
func getJobForUpdate(ctx context.Context,tx *sql.Tx,jobID string)(Job,error){return scanJob(tx.QueryRowContext(ctx,jobSelect+` WHERE j.id=UUID_TO_BIN(?,1) FOR UPDATE`,jobID))}
type rowScanner interface{Scan(...any)error}
func scanJob(row rowScanner)(Job,error){var job Job;var inputJSON,hash []byte;err:=row.Scan(&job.ID,&job.UserID,&job.IdempotencyKey,&inputJSON,&hash,&job.Status,&job.AttemptNo,&job.MaxAttempts,&job.AvailableAt,&job.LeaseOwner,&job.LeaseExpiresAt,&job.WorldID,&job.ErrorCode,&job.CreatedAt,&job.UpdatedAt);if errors.Is(err,sql.ErrNoRows){return Job{},ErrNotFound};if err!=nil{return Job{},fmt.Errorf("scan world draft job: %w",err)};if len(hash)!=32{return Job{},fmt.Errorf("invalid persisted world draft input hash")};copy(job.InputHash[:],hash);if err:=json.Unmarshal(inputJSON,&job.Input);err!=nil{return Job{},fmt.Errorf("decode world draft input: %w",err)};job.Kind="world_draft";return job,nil}

type rowQueryer interface{QueryRowContext(context.Context,string,...any)*sql.Row}
func loadWorld(ctx context.Context,q rowQueryer,ownerID,worldID string)(WorldResult,error){var world WorldResult;var setting []byte;err:=q.QueryRowContext(ctx,`SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(owner_id,1),title,summary,setting_json,visibility,status,version,draft_source,created_at,updated_at FROM worlds WHERE id=UUID_TO_BIN(?,1) AND owner_id=UUID_TO_BIN(?,1)`,worldID,ownerID).Scan(&world.ID,&world.OwnerID,&world.Title,&world.Summary,&setting,&world.Visibility,&world.Status,&world.Version,&world.DraftSource,&world.CreatedAt,&world.UpdatedAt);if errors.Is(err,sql.ErrNoRows){return WorldResult{},ErrNotFound};if err!=nil{return WorldResult{},fmt.Errorf("load generated world: %w",err)};world.Setting=append(json.RawMessage(nil),setting...);return world,nil}
func isDuplicate(err error)bool{var mysqlErr *mysql.MySQLError;return errors.As(err,&mysqlErr)&&mysqlErr.Number==1062}
