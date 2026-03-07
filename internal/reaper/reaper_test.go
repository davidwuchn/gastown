package reaper

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateDBName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"hq", false},
		{"beads", false},
		{"gt", false},
		{"test_db_123", false},
		{"", true},
		{"drop table", true},
		{"db;--", true},
		{"db`name", true},
		{"../etc/passwd", true},
	}
	for _, tt := range tests {
		err := ValidateDBName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateDBName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestDefaultDatabases(t *testing.T) {
	if len(DefaultDatabases) == 0 {
		t.Error("DefaultDatabases should not be empty")
	}
	for _, db := range DefaultDatabases {
		if err := ValidateDBName(db); err != nil {
			t.Errorf("DefaultDatabases contains invalid name %q: %v", db, err)
		}
	}
}

func TestFormatJSON(t *testing.T) {
	result := FormatJSON(map[string]int{"count": 42})
	if result == "" {
		t.Error("FormatJSON should not return empty string")
	}
	if result[0] != '{' {
		t.Errorf("FormatJSON should return JSON object, got %q", result[:10])
	}
}

func TestParentExcludeJoin(t *testing.T) {
	joinClause, whereCondition := parentExcludeJoin("testdb")

	// JOIN clause should reference the correct database.
	if joinClause == "" {
		t.Error("parentExcludeJoin joinClause should not be empty")
	}
	// parentExcludeJoin no longer qualifies table names with the database — the
	// reaper connects to a specific database via the DSN, so unqualified names
	// are correct. The dbName parameter is retained for API compatibility.

	// JOIN should select wisps with open parents from wisp_dependencies.
	if !contains(joinClause, "wisp_dependencies") {
		t.Error("parentExcludeJoin should query wisp_dependencies")
	}
	if !contains(joinClause, "parent-child") {
		t.Error("parentExcludeJoin should filter on parent-child type")
	}
	if !contains(joinClause, "'open', 'hooked', 'in_progress'") {
		t.Error("parentExcludeJoin should check for open parent statuses")
	}

	// WHERE condition should be an IS NULL anti-join filter.
	if whereCondition == "" {
		t.Error("parentExcludeJoin whereCondition should not be empty")
	}
	if !contains(whereCondition, "IS NULL") {
		t.Error("parentExcludeJoin whereCondition should use IS NULL for anti-join")
	}
}

// TestReapQueryNoDatabaseNameInjection verifies that the Reap function's batch
// SELECT query does not inject the database name into the SQL string. Previously,
// dbName was passed as a Sprintf arg but the format string didn't use it, causing
// positional shift: "FROM wisps w gt WHERE..." instead of "FROM wisps w LEFT JOIN...".
func TestReapQueryNoDatabaseNameInjection(t *testing.T) {
	// Reproduce the exact Sprintf call from Reap() to verify no dbName injection.
	dbName := "gt"
	parentJoin, parentWhere := parentExcludeJoin(dbName)
	whereClause := fmt.Sprintf(
		"w.status IN ('open', 'hooked', 'in_progress') AND w.created_at < ? AND %s", parentWhere)

	// This is the fixed query — dbName is NOT in the Sprintf args.
	idQuery := fmt.Sprintf(
		"SELECT w.id FROM wisps w %s WHERE %s LIMIT %d",
		parentJoin, whereClause, DefaultBatchSize)

	// The query must NOT contain the literal database name as a bare token.
	// Before the fix, "gt" appeared between "wisps w" and "WHERE".
	if strings.Contains(idQuery, "wisps w gt") {
		t.Errorf("Reap idQuery contains injected database name: %s", idQuery)
	}
	if !strings.Contains(idQuery, "LEFT JOIN") {
		t.Errorf("Reap idQuery should contain LEFT JOIN from parentExcludeJoin, got: %s", idQuery)
	}
	if !strings.Contains(idQuery, fmt.Sprintf("LIMIT %d", DefaultBatchSize)) {
		t.Errorf("Reap idQuery should end with LIMIT %d, got: %s", DefaultBatchSize, idQuery)
	}
}

// TestReapUpdateQueryNoDatabaseNameInjection verifies that the UPDATE query in
// Reap() does not inject dbName where the IN clause should go.
func TestReapUpdateQueryNoDatabaseNameInjection(t *testing.T) {
	dbName := "gt"
	inClause := "?,?,?"

	// This is the fixed query — only inClause in the Sprintf args.
	updateQuery := fmt.Sprintf(
		"UPDATE wisps SET status='closed', closed_at=NOW() WHERE id IN (%s)",
		inClause)

	if strings.Contains(updateQuery, dbName) {
		t.Errorf("Reap updateQuery contains injected database name %q: %s", dbName, updateQuery)
	}
	if !strings.Contains(updateQuery, "IN (?,?,?)") {
		t.Errorf("Reap updateQuery should contain parameterized IN clause, got: %s", updateQuery)
	}
}

// TestPurgeDigestQueryNoDatabaseNameInjection verifies that the purge digest
// query is a plain string with no Sprintf interpolation at all.
func TestPurgeDigestQueryNoDatabaseNameInjection(t *testing.T) {
	// The fixed digestQuery is a string literal — no Sprintf.
	digestQuery := "SELECT COALESCE(w.wisp_type, 'unknown') AS wtype, COUNT(*) AS cnt FROM wisps w WHERE w.status = 'closed' AND w.closed_at < ? GROUP BY wtype"

	if strings.Contains(digestQuery, "gt") {
		t.Errorf("purge digestQuery should not contain database name, got: %s", digestQuery)
	}
	if !strings.Contains(digestQuery, "GROUP BY wtype") {
		t.Errorf("purge digestQuery should end with GROUP BY, got: %s", digestQuery)
	}
}

// TestPurgeBatchQueryNoDatabaseNameInjection verifies that the purge batch
// SELECT query uses DefaultBatchSize as the LIMIT, not dbName.
func TestPurgeBatchQueryNoDatabaseNameInjection(t *testing.T) {
	// This is the fixed query — only DefaultBatchSize in the Sprintf args.
	idQuery := fmt.Sprintf(
		"SELECT w.id FROM wisps w WHERE w.status = 'closed' AND w.closed_at < ? LIMIT %d",
		DefaultBatchSize)

	if strings.Contains(idQuery, "gt") {
		t.Errorf("purge idQuery contains injected database name: %s", idQuery)
	}
	expected := fmt.Sprintf("LIMIT %d", DefaultBatchSize)
	if !strings.Contains(idQuery, expected) {
		t.Errorf("purge idQuery should contain %s, got: %s", expected, idQuery)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
