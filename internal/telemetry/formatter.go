package telemetry

import (
	"fmt"
	"strings"
)

// FormatForPrompt formats function telemetry into a markdown string
// suitable for inclusion in LLM prompts.
func FormatForPrompt(t *FunctionTelemetry) string {
	if t == nil {
		return ""
	}

	var sb strings.Builder

	// Invocation frequency
	if t.InvocationCount > 0 {
		sb.WriteString(fmt.Sprintf("This function is called ~%d times per collection period.\n", t.InvocationCount))
	}

	// Duration stats
	if t.AvgDurationUs > 0 || t.MaxDurationUs > 0 {
		sb.WriteString(fmt.Sprintf("Average duration: %.1fus, Max: %.1fus\n", t.AvgDurationUs, t.MaxDurationUs))
	}

	// Callers
	if len(t.Callers) > 0 {
		sb.WriteString(fmt.Sprintf("Called by: %s\n", strings.Join(t.Callers, ", ")))
	}

	// Endpoints
	if len(t.Endpoints) > 0 {
		sb.WriteString(fmt.Sprintf("Triggered by endpoints: %s\n", strings.Join(t.Endpoints, ", ")))
	}

	// Exceptions
	if len(t.Exceptions) > 0 {
		var parts []string
		for excType, count := range t.Exceptions {
			parts = append(parts, fmt.Sprintf("%s (%d occurrences)", excType, count))
		}
		sb.WriteString(fmt.Sprintf("Known exceptions: %s\n", strings.Join(parts, ", ")))
	}

	// Incidents
	if t.HasIncidents {
		sb.WriteString(fmt.Sprintf("Recent incidents: %s\n", t.IncidentSummary))
	}

	return sb.String()
}
