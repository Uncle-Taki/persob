package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/perso/perso-mind/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type JobsRepo struct {
	coll *mongo.Collection
}

func NewJobsRepo(db *DB) *JobsRepo {
	return &JobsRepo{
		coll: db.Database.Collection("processing_jobs"),
	}
}

func (r *JobsRepo) CreateJob(ctx context.Context, job *domain.ProcessingJob) error {
	now := time.Now()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	filter := bson.M{"job_id": job.JobID}
	opts := options.Replace().SetUpsert(true)
	_, err := r.coll.ReplaceOne(ctx, filter, job, opts)
	if err != nil {
		return fmt.Errorf("upsert processing job: %w", err)
	}
	return nil
}

func (r *JobsRepo) UpdateJobStatus(ctx context.Context, jobID, status, lastError string) error {
	filter := bson.M{"job_id": jobID}
	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"last_error": lastError,
			"updated_at": time.Now(),
		},
		"$inc": bson.M{"retry_count": 1},
	}
	_, err := r.coll.UpdateOne(ctx, filter, update)
	return err
}

func (r *JobsRepo) GetPendingOrFailedJobs(ctx context.Context, limit int64) ([]domain.ProcessingJob, error) {
	filter := bson.M{
		"status":      bson.M{"$in": []string{"pending", "failed"}},
		"retry_count": bson.M{"$lt": 5},
	}
	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "updated_at", Value: 1}})

	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find retryable jobs: %w", err)
	}
	defer cursor.Close(ctx)

	var jobs []domain.ProcessingJob
	if err := cursor.All(ctx, &jobs); err != nil {
		return nil, fmt.Errorf("decode jobs: %w", err)
	}
	return jobs, nil
}

func (r *JobsRepo) GetJob(ctx context.Context, jobID string) (*domain.ProcessingJob, error) {
	filter := bson.M{"job_id": jobID}
	var job domain.ProcessingJob
	err := r.coll.FindOne(ctx, filter).Decode(&job)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &job, nil
}
