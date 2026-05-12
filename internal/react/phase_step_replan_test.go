package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

func TestShouldSkipReplanLLM(t *testing.T) {
	tests := []struct {
		name     string
		outcome  *builtin_tools.StepOutcome
		snapshot builtin_tools.StateSnapshot
		want     bool
	}{
		{
			name:     "completed_no_signals_skip",
			outcome:  &builtin_tools.StepOutcome{Status: builtin_tools.StepOutcomeCompleted},
			snapshot: builtin_tools.StateSnapshot{},
			want:     true,
		},
		{
			name:    "completed_with_warnings_no_skip",
			outcome: &builtin_tools.StepOutcome{Status: builtin_tools.StepOutcomeCompleted},
			snapshot: builtin_tools.StateSnapshot{
				Warnings: []string{"something looks off"},
			},
			want: false,
		},
		{
			name:    "completed_with_unresolved_no_skip",
			outcome: &builtin_tools.StepOutcome{Status: builtin_tools.StepOutcomeCompleted},
			snapshot: builtin_tools.StateSnapshot{
				Unresolved: []string{"pending dependency"},
			},
			want: false,
		},
		{
			name: "completed_with_open_questions_no_skip",
			outcome: &builtin_tools.StepOutcome{
				Status:        builtin_tools.StepOutcomeCompleted,
				OpenQuestions: []string{"which API to use?"},
			},
			snapshot: builtin_tools.StateSnapshot{},
			want:     false,
		},
		{
			name:     "failed_no_skip",
			outcome:  &builtin_tools.StepOutcome{Status: builtin_tools.StepOutcomeFailed},
			snapshot: builtin_tools.StateSnapshot{},
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldSkipReplanLLM(tc.outcome, tc.snapshot)
			if got != tc.want {
				t.Errorf("shouldSkipReplanLLM() = %v, want %v", got, tc.want)
			}
		})
	}
}
