package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/perso/perso-mind/internal/domain"
)

type ProductRepo struct {
	db *DB
}

func NewProductRepo(db *DB) *ProductRepo {
	return &ProductRepo{db: db}
}

func (r *ProductRepo) Create(ctx context.Context, p *domain.Product) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	query := `
		INSERT INTO products (id, name, normalized_name, brand, manufacturer, model, category, attributes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at
	`
	attrs := p.Attributes
	if len(attrs) == 0 {
		attrs = []byte("{}")
	}

	return r.db.Pool.QueryRow(ctx, query,
		p.ID, p.Name, p.NormalizedName, p.Brand, p.Manufacturer, p.Model, p.Category, attrs,
	).Scan(&p.CreatedAt, &p.UpdatedAt)
}

func (r *ProductRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Product, error) {
	query := `
		SELECT id, name, normalized_name, brand, manufacturer, model, category, attributes, created_at, updated_at
		FROM products WHERE id = $1
	`
	var p domain.Product
	var manufacturer *string

	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&p.ID, &p.Name, &p.NormalizedName, &p.Brand, &manufacturer, &p.Model, &p.Category, &p.Attributes, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get product by id: %w", err)
	}
	if manufacturer != nil {
		p.Manufacturer = *manufacturer
	}

	// Fetch aliases
	aliases, err := r.GetAliasesByProductID(ctx, id)
	if err == nil {
		p.Aliases = aliases
	}

	return &p, nil
}

func (r *ProductRepo) List(ctx context.Context, limit, offset int, search, category string) ([]domain.Product, int64, error) {
	var countQuery string
	var query string
	var count int64

	args := []any{}
	paramIdx := 1

	whereClause := "WHERE 1=1"
	if search != "" {
		whereClause += fmt.Sprintf(" AND (name ILIKE $%d OR brand ILIKE $%d OR model ILIKE $%d)", paramIdx, paramIdx, paramIdx)
		args = append(args, "%"+search+"%")
		paramIdx++
	}
	if category != "" {
		whereClause += fmt.Sprintf(" AND category = $%d", paramIdx)
		args = append(args, category)
		paramIdx++
	}

	countQuery = "SELECT COUNT(*) FROM products " + whereClause
	if err := r.db.Pool.QueryRow(ctx, countQuery, args...).Scan(&count); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	query = fmt.Sprintf(`
		SELECT id, name, normalized_name, brand, manufacturer, model, category, attributes, created_at, updated_at
		FROM products %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, paramIdx, paramIdx+1)

	args = append(args, limit, offset)

	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	var products []domain.Product
	for rows.Next() {
		var p domain.Product
		var manufacturer *string
		if err := rows.Scan(&p.ID, &p.Name, &p.NormalizedName, &p.Brand, &manufacturer, &p.Model, &p.Category, &p.Attributes, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan product: %w", err)
		}
		if manufacturer != nil {
			p.Manufacturer = *manufacturer
		}
		products = append(products, p)
	}

	return products, count, nil
}

func (r *ProductRepo) GetAllWithAliases(ctx context.Context) ([]domain.Product, error) {
	products, _, err := r.List(ctx, 1000, 0, "", "")
	if err != nil {
		return nil, err
	}

	for i := range products {
		aliases, err := r.GetAliasesByProductID(ctx, products[i].ID)
		if err == nil {
			products[i].Aliases = aliases
		}
	}

	return products, nil
}

func (r *ProductRepo) GetAliasesByProductID(ctx context.Context, productID uuid.UUID) ([]domain.ProductAlias, error) {
	query := `
		SELECT id, product_id, alias, normalized_alias, locale, alias_type, created_at
		FROM product_aliases WHERE product_id = $1
	`
	rows, err := r.db.Pool.Query(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("get aliases: %w", err)
	}
	defer rows.Close()

	var aliases []domain.ProductAlias
	for rows.Next() {
		var a domain.ProductAlias
		if err := rows.Scan(&a.ID, &a.ProductID, &a.Alias, &a.NormalizedAlias, &a.Locale, &a.AliasType, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alias: %w", err)
		}
		aliases = append(aliases, a)
	}
	return aliases, nil
}

func (r *ProductRepo) AddAlias(ctx context.Context, alias *domain.ProductAlias) error {
	if alias.ID == uuid.Nil {
		alias.ID = uuid.New()
	}
	query := `
		INSERT INTO product_aliases (id, product_id, alias, normalized_alias, locale, alias_type)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at
	`
	return r.db.Pool.QueryRow(ctx, query,
		alias.ID, alias.ProductID, alias.Alias, alias.NormalizedAlias, alias.Locale, alias.AliasType,
	).Scan(&alias.CreatedAt)
}
