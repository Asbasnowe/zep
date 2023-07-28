package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/getzep/zep/pkg/llms"
	"github.com/getzep/zep/pkg/store"
	"github.com/pgvector/pgvector-go"
	"github.com/uptrace/bun"

	"github.com/getzep/zep/pkg/models"
)

const DefaultDocumentSearchLimit = 20

func newDocumentSearchOperation(
	ctx context.Context,
	appState *models.AppState,
	db *bun.DB,
	searchPayload *models.DocumentSearchPayload,
	collection *models.DocumentCollection,
	limit int,
	withMMR bool,
) *documentSearchOperation {
	if limit <= 0 {
		limit = DefaultDocumentSearchLimit
	}

	return &documentSearchOperation{
		ctx:           ctx,
		appState:      appState,
		db:            db,
		searchPayload: searchPayload,
		collection:    collection,
		limit:         limit,
		withMMR:       withMMR,
	}
}

type documentSearchOperation struct {
	ctx           context.Context
	appState      *models.AppState
	db            *bun.DB
	searchPayload *models.DocumentSearchPayload
	collection    *models.DocumentCollection
	queryVector   *[]float32
	limit         int
	withMMR       bool
}

func (dso *documentSearchOperation) Execute() (*models.DocumentSearchResultPage, error) {
	results := make([]models.DocumentSearchResult, 0)

	var count int
	var err error
	if dso.collection.IsIndexed {
		// run in transaction to set the ivfflat.probes setting
		err = dso.db.RunInTx(dso.ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
			_, err = dso.db.Exec("SET LOCAL ivfflat.probes = ?", dso.collection.ProbeCount)
			if err != nil {
				return fmt.Errorf("error setting probes: %w", err)
			}
			count, err = dso.execQuery(dso.db, &results)
			if err != nil {
				return fmt.Errorf("error executing query: %w", err)
			}

			return nil
		})
	} else {
		count, err = dso.execQuery(dso.db, &results)
		if err != nil {
			return &models.DocumentSearchResultPage{}, fmt.Errorf("error executing query: %w", err)
		}
	}

	resultPage := &models.DocumentSearchResultPage{
		Results:     results,
		ResultCount: count,
	}

	return resultPage, nil
}

// execQuery executes the query and scans the results into the provided results slice. It accepts a bun DB or Tx.
func (dso *documentSearchOperation) execQuery(
	db bun.IDB,
	results *[]models.DocumentSearchResult,
) (int, error) {
	query, err := dso.buildQuery(db)
	if err != nil {
		return 0, fmt.Errorf("error building query %w", err)
	}

	count, err := query.ScanAndCount(dso.ctx, results)
	if err != nil {
		return 0, fmt.Errorf("error scanning query %w", err)
	}

	if count == 0 {
		return 0, models.NewNotFoundError("no results found")
	}

	return count, nil
}

func (dso *documentSearchOperation) buildQuery(db bun.IDB) (*bun.SelectQuery, error) {
	m := &models.DocumentSearchResult{}
	query := db.NewSelect().Model(m).
		ModelTableExpr(dso.collection.TableName)

	// Add the vector column.
	if dso.searchPayload.Text != "" {
		v, err := dso.getDocQueryVector(dso.searchPayload.Text)
		if err != nil {
			return nil, fmt.Errorf("error getting query vector %w", err)
		}
		// Cosine distance is 1 - (a <=> b)
		query = query.ColumnExpr("1 - (embedding <=> ?) AS dist", v)
	}

	if len(dso.searchPayload.Metadata) > 0 {
		var err error
		query, err = dso.applyDocsMetadataFilter(query, dso.searchPayload.Metadata)
		if err != nil {
			return nil, store.NewStorageError("error applying metadata filter", err)
		}
	}

	// Add LIMIT
	// If we're using MMR, we need to add a limit of 2x the requested limit to allow for the MMR
	// algorithm to rerank and filter out results.
	limit := dso.limit
	if dso.withMMR {
		limit *= 2
	}
	query = query.Limit(dso.limit)

	// Order by dist - required for index to be used.
	if dso.searchPayload.Text != "" {
		query.Order("dist ASC")
	}

	return query, nil
}

// getDocQueryVector returns the vector for the query text.
func (dso *documentSearchOperation) getDocQueryVector(
	queryText string,
) (pgvector.Vector, error) {
	documentType := "document"
	model, err := llms.GetMessageEmbeddingModel(dso.appState, documentType)
	if err != nil {
		return pgvector.Vector{}, fmt.Errorf("failed to get document embedding model %w", err)
	}

	e, err := llms.EmbedTexts(dso.ctx, dso.appState, model, documentType, []string{queryText})
	if err != nil {
		return pgvector.Vector{}, fmt.Errorf("failed to embed query %w", err)
	}

	dso.queryVector = &e[0]

	v := pgvector.NewVector(e[0])
	return v, nil
}

// applyDocsMetadataFilter applies the metadata filter to the query.
func (dso *documentSearchOperation) applyDocsMetadataFilter(
	query *bun.SelectQuery,
	metadata map[string]interface{},
) (*bun.SelectQuery, error) {
	qb := query.QueryBuilder()

	if where, ok := metadata["where"]; ok {
		j, err := json.Marshal(where)
		if err != nil {
			return nil, fmt.Errorf("error marshalling metadata %w", err)
		}

		var jq JSONQuery
		err = json.Unmarshal(j, &jq)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling metadata %w", err)
		}
		qb = parseJSONQuery(qb, &jq, false)
	}

	query = qb.Unwrap().(*bun.SelectQuery)

	return query, nil
}
