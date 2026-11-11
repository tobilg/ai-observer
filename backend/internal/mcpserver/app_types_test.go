package mcpserver

import (
	"strings"
	"testing"
)

// UsageGroups, from which the view's types are generated, are the values
// usage_summary accepts
func TestUsageGroupsAreAccepted(t *testing.T) {
	cs, _, _ := testSession(t)
	for _, group := range UsageGroups {
		var out usageSummaryOut
		if errText := call(t, cs, "usage_summary", map[string]any{"group_by": group}, &out); errText != "" {
			t.Errorf("group_by %s: %s", group, errText)
		} else if out.GroupBy != group {
			t.Errorf("group_by %s returned %q", group, out.GroupBy)
		}
	}
	errText := call(t, cs, "usage_summary", map[string]any{"group_by": "week"}, nil)
	if !strings.Contains(errText, "invalid group_by") {
		t.Errorf("group_by week: %q, want an error", errText)
	}
	var out usageSummaryOut
	call(t, cs, "usage_summary", map[string]any{}, &out)
	if out.GroupBy != UsageGroups[0] {
		t.Errorf("default group_by = %q, want %q", out.GroupBy, UsageGroups[0])
	}
}
