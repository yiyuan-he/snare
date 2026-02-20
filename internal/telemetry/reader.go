package telemetry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// FunctionTelemetry holds production telemetry data for a single function.
type FunctionTelemetry struct {
	FunctionName    string
	FilePath        string
	InvocationCount int
	AvgDurationUs   float64
	MaxDurationUs   float64
	Callers         []string
	Exceptions      map[string]int
	Endpoints       []string
	HasIncidents    bool
	IncidentSummary string
}

// Reader reads telemetry data from a SQLite database populated by the Telemend SDK.
type Reader struct {
	db *sql.DB
}

// NewReader opens a SQLite database for reading telemetry.
func NewReader(dbPath string) (*Reader, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening telemetry database: %w", err)
	}

	// Verify the database has the expected tables
	if err := verifySchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("verifying telemetry schema: %w", err)
	}

	return &Reader{db: db}, nil
}

// Close closes the database connection.
func (r *Reader) Close() error {
	return r.db.Close()
}

// verifySchema checks that the expected tables exist.
func verifySchema(db *sql.DB) error {
	tables := []string{"function_mapping", "function_calls"}
	for _, t := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", t).Scan(&name)
		if err != nil {
			return fmt.Errorf("table %q not found: %w", t, err)
		}
	}
	return nil
}

// GetFunctionTelemetry retrieves aggregated telemetry for a function by name and file path.
// Uses path suffix matching to handle absolute vs relative paths.
func (r *Reader) GetFunctionTelemetry(funcName string, filePath string) (*FunctionTelemetry, error) {
	// Find the function_id(s) matching this function name and file path
	funcIDs, mappedPath, err := r.findFunctionIDs(funcName, filePath)
	if err != nil {
		return nil, err
	}
	if len(funcIDs) == 0 {
		return nil, nil // No telemetry data for this function
	}

	ft := &FunctionTelemetry{
		FunctionName: funcName,
		FilePath:     mappedPath,
		Exceptions:   make(map[string]int),
	}

	// Aggregate function call metrics across all matching function_ids
	for _, fid := range funcIDs {
		if err := r.aggregateCalls(fid, ft); err != nil {
			return nil, fmt.Errorf("aggregating calls for %s: %w", fid, err)
		}
	}

	// Look up incidents
	if err := r.findIncidents(funcIDs, ft); err != nil {
		return nil, fmt.Errorf("finding incidents: %w", err)
	}

	return ft, nil
}

// findFunctionIDs looks up function_ids from function_mapping by name and path suffix.
func (r *Reader) findFunctionIDs(funcName string, filePath string) ([]string, string, error) {
	// Try exact name match first, then use path suffix matching
	rows, err := r.db.Query(
		"SELECT function_id, file_path FROM function_mapping WHERE name = ?",
		funcName,
	)
	if err != nil {
		return nil, "", fmt.Errorf("querying function_mapping: %w", err)
	}
	defer rows.Close()

	var ids []string
	var matchedPath string
	basePath := filepath.Base(filePath)

	for rows.Next() {
		var fid, fpath string
		if err := rows.Scan(&fid, &fpath); err != nil {
			return nil, "", err
		}

		// Path suffix matching: check if the mapped file path ends with
		// the same base filename, handling absolute vs relative differences
		mappedBase := filepath.Base(fpath)
		if mappedBase == basePath || strings.HasSuffix(fpath, filePath) || strings.HasSuffix(filePath, fpath) {
			ids = append(ids, fid)
			matchedPath = fpath
		}
	}

	return ids, matchedPath, rows.Err()
}

// aggregateCalls sums up function call metrics for a given function_id.
func (r *Reader) aggregateCalls(funcID string, ft *FunctionTelemetry) error {
	rows, err := r.db.Query(
		`SELECT endpoint_id, caller, exceptions, duration_count, duration_sum, duration_max, duration_min
		 FROM function_calls WHERE function_id = ?`,
		funcID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	endpointSet := make(map[string]bool)
	callerSet := make(map[string]bool)
	var totalCount int
	var totalSum float64
	var maxDuration float64

	for rows.Next() {
		var endpointID, caller, exceptionsJSON sql.NullString
		var dCount, dSum, dMax, dMin sql.NullFloat64

		if err := rows.Scan(&endpointID, &caller, &exceptionsJSON, &dCount, &dSum, &dMax, &dMin); err != nil {
			return err
		}

		if dCount.Valid {
			totalCount += int(dCount.Float64)
		}
		if dSum.Valid {
			totalSum += dSum.Float64
		}
		if dMax.Valid && dMax.Float64 > maxDuration {
			maxDuration = dMax.Float64
		}

		if endpointID.Valid && endpointID.String != "" {
			endpointSet[endpointID.String] = true
		}
		if caller.Valid && caller.String != "" {
			callerSet[caller.String] = true
		}

		// Parse exceptions JSON
		if exceptionsJSON.Valid && exceptionsJSON.String != "" && exceptionsJSON.String != "{}" {
			var exceptions map[string]int
			if err := json.Unmarshal([]byte(exceptionsJSON.String), &exceptions); err == nil {
				for k, v := range exceptions {
					ft.Exceptions[k] += v
				}
			}
		}
	}

	ft.InvocationCount += totalCount
	if totalCount > 0 {
		ft.AvgDurationUs = totalSum / float64(totalCount)
	}
	if maxDuration > ft.MaxDurationUs {
		ft.MaxDurationUs = maxDuration
	}

	// Resolve endpoint IDs to routes
	for eid := range endpointSet {
		route, err := r.resolveEndpoint(eid)
		if err == nil && route != "" {
			ft.Endpoints = append(ft.Endpoints, route)
		} else {
			ft.Endpoints = append(ft.Endpoints, eid)
		}
	}

	// Resolve caller function IDs to names
	for cid := range callerSet {
		name, err := r.resolveFunctionName(cid)
		if err == nil && name != "" {
			ft.Callers = append(ft.Callers, name)
		} else {
			ft.Callers = append(ft.Callers, cid)
		}
	}

	return rows.Err()
}

// resolveEndpoint looks up the route for an endpoint_id.
func (r *Reader) resolveEndpoint(endpointID string) (string, error) {
	var method, route sql.NullString
	err := r.db.QueryRow(
		"SELECT method, route FROM endpoint_metrics WHERE endpoint_id = ? LIMIT 1",
		endpointID,
	).Scan(&method, &route)
	if err != nil {
		return "", err
	}
	if method.Valid && route.Valid {
		return method.String + " " + route.String, nil
	}
	return "", nil
}

// resolveFunctionName looks up the name for a function_id.
func (r *Reader) resolveFunctionName(funcID string) (string, error) {
	var name string
	err := r.db.QueryRow(
		"SELECT name FROM function_mapping WHERE function_id = ?",
		funcID,
	).Scan(&name)
	if err != nil {
		return "", err
	}
	return name, nil
}

// findIncidents checks for incidents involving the given function IDs.
func (r *Reader) findIncidents(funcIDs []string, ft *FunctionTelemetry) error {
	// Check incident_snapshots table — look for incidents where the call_path
	// contains any of our function IDs or where exception relates to the function
	rows, err := r.db.Query(
		"SELECT trigger_type, affected_endpoint, exception_type, exception_message FROM incident_snapshots",
	)
	if err != nil {
		// Table might not exist — not fatal
		return nil
	}
	defer rows.Close()

	var incidents []string
	funcIDSet := make(map[string]bool)
	for _, fid := range funcIDs {
		funcIDSet[fid] = true
	}

	for rows.Next() {
		var triggerType, endpoint, exType, exMsg sql.NullString
		if err := rows.Scan(&triggerType, &endpoint, &exType, &exMsg); err != nil {
			continue
		}

		// Include all incidents as context for now
		desc := ""
		if triggerType.Valid {
			desc = triggerType.String
		}
		if endpoint.Valid && endpoint.String != "" {
			desc += " on " + endpoint.String
		}
		if exType.Valid && exType.String != "" {
			desc += " (" + exType.String + ")"
		}
		incidents = append(incidents, desc)
	}

	if len(incidents) > 0 {
		ft.HasIncidents = true
		if len(incidents) <= 3 {
			ft.IncidentSummary = strings.Join(incidents, "; ")
		} else {
			ft.IncidentSummary = fmt.Sprintf("%d incidents: %s", len(incidents), strings.Join(incidents[:3], "; "))
		}
	}

	return nil
}
