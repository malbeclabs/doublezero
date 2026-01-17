package dataset

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLake_Clickhouse_Dataset_DimensionType2_Typed(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	ctx := t.Context()

	createSinglePKTables(t, conn)

	d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
	require.NoError(t, err)
	// Define a struct type matching the schema
	type Contributor struct {
		PK   string `ch:"pk"`
		Code string `ch:"code"`
		Name string `ch:"name"`
	}

	// Test typed writer and reader
	t.Run("write_and_read_typed_structs", func(t *testing.T) {
		typed := NewTypedDimensionType2Dataset[Contributor](d)

		rows := []Contributor{
			{PK: "entity1", Code: "CODE1", Name: "Name1"},
			{PK: "entity2", Code: "CODE2", Name: "Name2"},
			{PK: "entity3", Code: "CODE3", Name: "Name3"},
		}

		err := typed.WriteBatch(ctx, conn, rows)
		require.NoError(t, err)
		// Verify data was written correctly by reading it back with typed reader
		entityID1 := string(NewNaturalKey("entity1").ToSurrogate())
		current1, err := typed.GetCurrentRow(ctx, conn, SurrogateKey(entityID1))
		require.NoError(t, err)
		require.NotNil(t, current1)
		require.Equal(t, "CODE1", current1.Code)
		require.Equal(t, "Name1", current1.Name)

		entityID2 := string(NewNaturalKey("entity2").ToSurrogate())
		current2, err := typed.GetCurrentRow(ctx, conn, SurrogateKey(entityID2))
		require.NoError(t, err)
		require.NotNil(t, current2)
		require.Equal(t, "CODE2", current2.Code)
		require.Equal(t, "Name2", current2.Name)

		// Test GetCurrentRows
		entityIDs := []SurrogateKey{SurrogateKey(entityID1), SurrogateKey(entityID2)}
		allRows, err := typed.GetCurrentRows(ctx, conn, entityIDs)
		require.NoError(t, err)
		require.Len(t, allRows, 2)
	})

	// Test typed writer with snake_case field names (automatic conversion)
	t.Run("write_with_snake_case_fields", func(t *testing.T) {
		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})

		require.NoError(t, err)
		type ContributorSnake struct {
			PK   string // Will match "pk" via snake_case conversion
			Code string // Will match "code" via snake_case conversion
			Name string // Will match "name" via snake_case conversion
		}

		typed := NewTypedDimensionType2Dataset[ContributorSnake](d)

		rows := []ContributorSnake{
			{PK: "entity4", Code: "CODE4", Name: "Name4"},
		}

		err = typed.WriteBatch(ctx, conn, rows)
		require.NoError(t, err)
		// Verify data was written correctly using typed reader
		entityID := string(NewNaturalKey("entity4").ToSurrogate())
		current, err := typed.GetCurrentRow(ctx, conn, SurrogateKey(entityID))
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, "CODE4", current.Code)
		require.Equal(t, "Name4", current.Name)
	})

	// Test typed writer with CamelCase field names (automatic conversion)
	t.Run("write_with_camelcase_fields", func(t *testing.T) {
		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})

		require.NoError(t, err)
		type ContributorCamel struct {
			Pk   string // Will match "pk" via camelCase conversion
			Code string // Will match "code" directly
			Name string // Will match "name" directly
		}

		typed := NewTypedDimensionType2Dataset[ContributorCamel](d)

		rows := []ContributorCamel{
			{Pk: "entity5", Code: "CODE5", Name: "Name5"},
		}

		err = typed.WriteBatch(ctx, conn, rows)
		require.NoError(t, err)
		// Verify data was written correctly using typed reader
		entityID := string(NewNaturalKey("entity5").ToSurrogate())
		current, err := typed.GetCurrentRow(ctx, conn, SurrogateKey(entityID))
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, "CODE5", current.Code)
		require.Equal(t, "Name5", current.Name)
	})
}
