package gentoo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConditionExpr_String(t *testing.T) {
	tests := []struct {
		name     string
		expr     conditionExpr
		expected string
	}{
		{
			name:     "ArchExpr",
			expr:     ArchExpr{Arch: "amd64"},
			expected: "Arch(amd64)",
		},
		{
			name:     "UseExpr",
			expr:     UseExpr{Flag: "extended"},
			expected: "Use(extended)",
		},
		{
			name:     "NotExpr of Use",
			expr:     NewNotExpr(UseExpr{Flag: "extended"}),
			expected: "NOT(Use(extended))",
		},
		{
			name:     "NotExpr of Arch",
			expr:     NewNotExpr(ArchExpr{Arch: "arm64"}),
			expected: "NOT(Arch(arm64))",
		},
		{
			name: "AndExpr canonical sorting",
			expr: NewAndExpr(
				UseExpr{Flag: "extended"},
				ArchExpr{Arch: "arm64"},
			),
			expected: "AND(Arch(arm64), Use(extended))",
		},
		{
			name: "AndExpr with NOT and Arch",
			expr: NewAndExpr(
				NewNotExpr(UseExpr{Flag: "extended"}),
				ArchExpr{Arch: "arm64"},
			),
			expected: "AND(Arch(arm64), NOT(Use(extended)))",
		},
		{
			name: "OrExpr canonical sorting",
			expr: NewOrExpr(
				ArchExpr{Arch: "arm64"},
				ArchExpr{Arch: "amd64"},
			),
			expected: "OR(Arch(amd64), Arch(arm64))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.expr.String())
		})
	}
}

func TestConditionExpr_Shell(t *testing.T) {
	tests := []struct {
		name     string
		expr     conditionExpr
		expected string
	}{
		{
			name:     "ArchExpr",
			expr:     ArchExpr{Arch: "amd64"},
			expected: "use amd64",
		},
		{
			name:     "UseExpr",
			expr:     UseExpr{Flag: "extended"},
			expected: "use extended",
		},
		{
			name:     "NotExpr of Use",
			expr:     NewNotExpr(UseExpr{Flag: "extended"}),
			expected: "! use extended",
		},
		{
			name: "AndExpr with Arch and NOT Use",
			expr: NewAndExpr(
				ArchExpr{Arch: "arm64"},
				NewNotExpr(UseExpr{Flag: "extended"}),
			),
			expected: "use arm64 && ! use extended",
		},
		{
			name: "OrExpr with multiple Archs",
			expr: NewOrExpr(
				ArchExpr{Arch: "amd64"},
				ArchExpr{Arch: "arm64"},
			),
			expected: "use amd64 || use arm64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.expr.Shell())
		})
	}
}

func TestConditionExpr_Algebra(t *testing.T) {
	t.Run("Double negation elimination", func(t *testing.T) {
		orig := UseExpr{Flag: "extended"}
		not1 := NewNotExpr(orig)
		not2 := NewNotExpr(not1)
		require.Equal(t, orig, not2)
		require.Equal(t, "Use(extended)", not2.String())
	})

	t.Run("Nested AND flattening", func(t *testing.T) {
		a := ArchExpr{Arch: "amd64"}
		b := UseExpr{Flag: "foo"}
		c := UseExpr{Flag: "bar"}
		nested := NewAndExpr(a, NewAndExpr(b, c))
		require.Equal(t, "AND(Arch(amd64), Use(bar), Use(foo))", nested.String())
	})

	t.Run("Nested OR flattening", func(t *testing.T) {
		a := ArchExpr{Arch: "amd64"}
		b := ArchExpr{Arch: "arm64"}
		c := ArchExpr{Arch: "riscv"}
		nested := NewOrExpr(a, NewOrExpr(b, c))
		require.Equal(t, "OR(Arch(amd64), Arch(arm64), Arch(riscv))", nested.String())
	})

	t.Run("AND deduplication", func(t *testing.T) {
		a := ArchExpr{Arch: "amd64"}
		b := UseExpr{Flag: "foo"}
		and := NewAndExpr(a, b, a, b)
		require.Equal(t, "AND(Arch(amd64), Use(foo))", and.String())
	})

	t.Run("OR deduplication", func(t *testing.T) {
		a := ArchExpr{Arch: "amd64"}
		b := ArchExpr{Arch: "arm64"}
		or := NewOrExpr(a, b, a)
		require.Equal(t, "OR(Arch(amd64), Arch(arm64))", or.String())
	})

	t.Run("Single term simplification", func(t *testing.T) {
		a := ArchExpr{Arch: "amd64"}
		and := NewAndExpr(a)
		or := NewOrExpr(a)
		require.Equal(t, a, and)
		require.Equal(t, a, or)
	})
}

func TestUniversalArchDetection(t *testing.T) {
	universe := []string{"amd64", "arm64"}

	require.True(t, isUniversalArchExpr(NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"}), universe))
	require.False(t, isUniversalArchExpr(ArchExpr{Arch: "amd64"}, universe))
	require.False(t, isUniversalArchExpr(UseExpr{Flag: "amd64"}, universe))
	require.False(t, isUniversalArchExpr(NewOrExpr(ArchExpr{Arch: "amd64"}, UseExpr{Flag: "arm64"}), universe))

	singleUniverse := []string{"amd64"}
	require.True(t, isUniversalArchExpr(ArchExpr{Arch: "amd64"}, singleUniverse))
	require.False(t, isUniversalArchExpr(UseExpr{Flag: "amd64"}, singleUniverse))
}

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
					Expr: newArchsAndUseExpr([]string{"amd64", "arm64"}, nil),
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
					Expr: newArchsAndUseExpr([]string{"amd64"}, nil),
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
			},
			expected: []installStmt{
				conditionStmt{
					Expr: newArchsAndUseExpr([]string{"amd64"}, nil),
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
					Expr: newArchsAndUseExpr([]string{"amd64"}, nil),
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
					Expr: newArchsAndUseExpr([]string{"amd64"}, nil),
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
					Expr: newArchsAndUseExpr([]string{"amd64"}, nil),
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
					Expr: newArchsAndUseExpr([]string{"amd64"}, nil),
					Body: []installStmt{
						stateStmt{Command: "exeinto", Value: "/usr/bin"},
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
				stateStmt{Command: "exeinto", Value: "/opt/bin"},
				actionStmt{Command: "doexe", Source: "bar"},
			},
		},
		{
			name: "exact sibling predicates merge",
			input: []installStmt{
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "prog1"},
					},
				},
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "prog2"},
					},
				},
			},
			expected: []installStmt{
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "prog1"},
						actionStmt{Command: "doexe", Source: "prog2"},
					},
				},
			},
		},
		{
			name: "recursive factoring of sibling statement conditions into nested conditions",
			input: []installStmt{
				conditionStmt{
					Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, UseExpr{Flag: "extended"}),
					Body: []installStmt{actionStmt{Command: "newexe", Source: "prog1_x86", Target: "prog1"}},
				},
				conditionStmt{
					Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, NewNotExpr(UseExpr{Flag: "extended"})),
					Body: []installStmt{actionStmt{Command: "newexe", Source: "prog2_x86", Target: "prog2"}},
				},
				conditionStmt{
					Expr: NewAndExpr(ArchExpr{Arch: "arm64"}, UseExpr{Flag: "extended"}),
					Body: []installStmt{actionStmt{Command: "newexe", Source: "prog1_arm", Target: "prog1"}},
				},
				conditionStmt{
					Expr: NewAndExpr(ArchExpr{Arch: "arm64"}, NewNotExpr(UseExpr{Flag: "extended"})),
					Body: []installStmt{actionStmt{Command: "newexe", Source: "prog2_arm", Target: "prog2"}},
				},
			},
			expected: []installStmt{
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						conditionStmt{
							Expr: UseExpr{Flag: "extended"},
							Body: []installStmt{actionStmt{Command: "newexe", Source: "prog1_x86", Target: "prog1"}},
						},
						conditionStmt{
							Expr: NewNotExpr(UseExpr{Flag: "extended"}),
							Body: []installStmt{actionStmt{Command: "newexe", Source: "prog2_x86", Target: "prog2"}},
						},
					},
				},
				conditionStmt{
					Expr: ArchExpr{Arch: "arm64"},
					Body: []installStmt{
						conditionStmt{
							Expr: UseExpr{Flag: "extended"},
							Body: []installStmt{actionStmt{Command: "newexe", Source: "prog1_arm", Target: "prog1"}},
						},
						conditionStmt{
							Expr: NewNotExpr(UseExpr{Flag: "extended"}),
							Body: []installStmt{actionStmt{Command: "newexe", Source: "prog2_arm", Target: "prog2"}},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &installPlan{
				UniverseArchitectures: universe,
				Body:                  tt.input,
			}
			reduced := plan.reducePlan()
			require.Equal(t, tt.expected, reduced.Body)
		})
	}
}

func TestReducer_Idempotence(t *testing.T) {
	plan := &installPlan{
		UniverseArchitectures: []string{"amd64", "arm64", "riscv"},
		Body: []installStmt{
			stateStmt{Command: "exeinto", Value: "/opt/bin"},
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, UseExpr{Flag: "extended"}),
				Body: []installStmt{actionStmt{Command: "newexe", Source: "p1_x86", Target: "p1"}},
			},
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, NewNotExpr(UseExpr{Flag: "extended"})),
				Body: []installStmt{actionStmt{Command: "newexe", Source: "p2_x86", Target: "p2"}},
			},
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "arm64"}, UseExpr{Flag: "extended"}),
				Body: []installStmt{actionStmt{Command: "newexe", Source: "p1_arm", Target: "p1"}},
			},
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "arm64"}, NewNotExpr(UseExpr{Flag: "extended"})),
				Body: []installStmt{actionStmt{Command: "newexe", Source: "p2_arm", Target: "p2"}},
			},
		},
	}

	once := plan.reducePlan()
	twice := once.reducePlan()

	require.Equal(t, once, twice)
}

func TestReducer_NestedRendering(t *testing.T) {
	plan := &installPlan{
		UniverseArchitectures: []string{"amd64", "arm64"},
		Body: []installStmt{
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, UseExpr{Flag: "extended"}),
				Body: []installStmt{actionStmt{Command: "newexe", Source: "prog1_x86", Target: "prog1", Die: "Failed to install binary"}},
			},
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, NewNotExpr(UseExpr{Flag: "extended"})),
				Body: []installStmt{actionStmt{Command: "newexe", Source: "prog2_x86", Target: "prog2", Die: "Failed to install binary"}},
			},
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "arm64"}, UseExpr{Flag: "extended"}),
				Body: []installStmt{actionStmt{Command: "newexe", Source: "prog1_arm", Target: "prog1", Die: "Failed to install binary"}},
			},
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "arm64"}, NewNotExpr(UseExpr{Flag: "extended"})),
				Body: []installStmt{actionStmt{Command: "newexe", Source: "prog2_arm", Target: "prog2", Die: "Failed to install binary"}},
			},
		},
	}

	reduced := plan.reducePlan()
	output := formatStmts(reduced.Body, "  ")

	expected := `  if use amd64; then
    if use extended; then
      newexe "prog1_x86" "prog1" || die "Failed to install binary"
    fi
    if ! use extended; then
      newexe "prog2_x86" "prog2" || die "Failed to install binary"
    fi
  fi
  if use arm64; then
    if use extended; then
      newexe "prog1_arm" "prog1" || die "Failed to install binary"
    fi
    if ! use extended; then
      newexe "prog2_arm" "prog2" || die "Failed to install binary"
    fi
  fi`

	require.Equal(t, expected, output)
}

func TestReducer_Validation(t *testing.T) {
	t.Run("valid plan", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{Command: "dosym", Source: "src", Target: "dst"},
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						actionStmt{Command: "doexe", Source: "foo"},
					},
				},
			},
		}
		require.NoError(t, plan.Validate())
	})

	t.Run("nested invalid dosym", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						conditionStmt{
							Expr: UseExpr{Flag: "foo"},
							Body: []installStmt{
								actionStmt{Command: "dosym", Source: "src", Target: ""},
							},
						},
					},
				},
			},
		}
		require.EqualError(t, plan.Validate(), "dosym requires a destination")
	})
}
