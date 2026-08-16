package gentoo

import (
	"reflect"
	"testing"
)

func TestReducer(t *testing.T) {
	universe := []string{"amd64", "arm64"}

	tests := []struct {
		name     string
		input    []installStmt
		expected []installStmt
	}{
		{
			name: "universal architecture condition -> removed",
			input: []installStmt{
				conditionStmt{
					Architectures: []string{"amd64", "arm64"},
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
			},
			expected: []installStmt{
				actionStmt{Command: "doexe", Source: "foo"},
			},
		},
		{
			name: "partial architecture condition -> retained",
			input: []installStmt{
				conditionStmt{
					Architectures: []string{"amd64"},
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
			},
			expected: []installStmt{
				conditionStmt{
					Architectures: []string{"amd64"},
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
			},
		},
		{
			name: "redundant state setter -> removed",
			input: []installStmt{
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				actionStmt{Command: "doexe", Source: "foo"},
			},
			expected: []installStmt{
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				actionStmt{Command: "doexe", Source: "foo"},
			},
		},
		{
			name: "non-mutating conditional preserves incoming state",
			input: []installStmt{
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				conditionStmt{
					Architectures: []string{"amd64"},
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				actionStmt{Command: "doexe", Source: "bar"},
			},
			expected: []installStmt{
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				conditionStmt{
					Architectures: []string{"amd64"},
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
				actionStmt{Command: "doexe", Source: "bar"},
			},
		},
		{
			name: "conditional state change causes divergent/unknown outgoing state",
			input: []installStmt{
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				conditionStmt{
					Architectures: []string{"amd64"},
					Body: []installStmt{
						stateStmt{Command: "exeinto", Value: "/usr/bin"},
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				actionStmt{Command: "doexe", Source: "bar"},
			},
			expected: []installStmt{
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				conditionStmt{
					Architectures: []string{"amd64"},
					Body: []installStmt{
						stateStmt{Command: "exeinto", Value: "/usr/bin"},
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				actionStmt{Command: "doexe", Source: "bar"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := installPlan{
				UniverseArchitectures: universe,
				Body:                  tt.input,
			}
			reduced := reducePlan(plan)
			if !reflect.DeepEqual(reduced.Body, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, reduced.Body)
			}
		})
	}
}
