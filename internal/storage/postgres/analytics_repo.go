package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/domain"
)

type AnalyticsRepo struct {
	db *DB
}

func NewAnalyticsRepo(db *DB) *AnalyticsRepo {
	return &AnalyticsRepo{db: db}
}

func (r *AnalyticsRepo) UpsertAggregate(ctx context.Context, agg *domain.PriceStatistics) error {
	query := `
		INSERT INTO price_aggregates (
			product_id, min_price, max_price, avg_price, median_price, price_range, sample_count, currency, period, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, 'all_time', NOW()
		)
		ON CONFLICT (product_id, period, currency) DO UPDATE SET
			min_price = EXCLUDED.min_price,
			max_price = EXCLUDED.max_price,
			avg_price = EXCLUDED.avg_price,
			median_price = EXCLUDED.median_price,
			price_range = EXCLUDED.price_range,
			sample_count = EXCLUDED.sample_count,
			updated_at = NOW()
	`
	_, err := r.db.Pool.Exec(ctx, query,
		agg.ProductID, agg.MinPrice, agg.MaxPrice, agg.AvgPrice, agg.MedianPrice, agg.PriceRange, agg.SampleCount, agg.Currency,
	)
	return err
}

func (r *AnalyticsRepo) GetProductAggregate(ctx context.Context, productID uuid.UUID, currency domain.Currency) (*domain.PriceStatistics, error) {
	query := `
		SELECT pa.product_id, p.name, pa.currency, pa.min_price, pa.max_price, pa.avg_price, pa.median_price, pa.price_range, pa.sample_count, pa.updated_at
		FROM price_aggregates pa
		JOIN products p ON pa.product_id = p.id
		WHERE pa.product_id = $1 AND pa.currency = $2 AND pa.period = 'all_time'
	`
	var stat domain.PriceStatistics
	err := r.db.Pool.QueryRow(ctx, query, productID, currency).Scan(
		&stat.ProductID, &stat.ProductName, &stat.Currency, &stat.MinPrice, &stat.MaxPrice,
		&stat.AvgPrice, &stat.MedianPrice, &stat.PriceRange, &stat.SampleCount, &stat.LastCalculated,
	)
	if err != nil {
		return nil, err
	}
	stat.Methodology = "Calculated from non-outlier confirmed seller offers in IRR_TOMAN"
	return &stat, nil
}

func (r *AnalyticsRepo) GetHistoricalPrices(ctx context.Context, productID uuid.UUID, since time.Time) ([]domain.PriceObservation, error) {
	query := `
		SELECT id, product_id, offer_id, price_amount, price_currency, observed_at, created_at
		FROM price_observations
		WHERE product_id = $1 AND observed_at >= $2
		ORDER BY observed_at ASC
	`
	rows, err := r.db.Pool.Query(ctx, query, productID, since)
	if err != nil {
		return nil, fmt.Errorf("query historical prices: %w", err)
	}
	defer rows.Close()

	var observations []domain.PriceObservation
	for rows.Next() {
		var o domain.PriceObservation
		if err := rows.Scan(&o.ID, &o.ProductID, &o.OfferID, &o.PriceAmount, &o.PriceCurrency, &o.ObservedAt, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}
		observations = append(observations, o)
	}
	return observations, nil
}

func (r *AnalyticsRepo) GetDashboardSummary(ctx context.Context) (*domain.DashboardSummary, error) {
	var summary domain.DashboardSummary

	err := r.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM products").Scan(&summary.TotalTrackedProducts)
	if err != nil {
		return nil, fmt.Errorf("count products: %w", err)
	}

	err = r.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM offers WHERE listing_status = 'active'").Scan(&summary.TotalActiveOffers)
	if err != nil {
		return nil, fmt.Errorf("count active offers: %w", err)
	}

	err = r.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM watch_sources WHERE monitoring_status = 'active'").Scan(&summary.TotalWatchSources)
	if err != nil {
		return nil, fmt.Errorf("count watch sources: %w", err)
	}

	err = r.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM conversations WHERE state IN ('NEW', 'AWAITING_APPROVAL', 'ACTIVE', 'WAITING_FOR_REPLY')").Scan(&summary.ActiveConversations)
	if err != nil {
		return nil, fmt.Errorf("count active conversations: %w", err)
	}

	return &summary, nil
}
