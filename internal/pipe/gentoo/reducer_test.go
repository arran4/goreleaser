package gentoo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConditionExprPrecedenceShellAndString(t *testing.T) {
	a := UseExpr{Flag: "a"}
	b := UseExpr{Flag: "b"}
	c := UseExpr{Flag: "c"}
	d := UseExpr{Flag: "d"}

	testCases := []struct {
		name     string
		expr     conditionExpr
		expected string
		shell    string
	}{
		{
			name:     "atomic Arch",
			expr:     ArchExpr{Arch: "amd64"},
			expected: "Arch(amd64)",
			shell:    "use amd64",
		},
		{
			name:     "atomic Use",
			expr:     UseExpr{Flag: "extended"},
			expected: "Use(extended)",
			shell:    "use extended",
		},
		{
			name:     "NOT atomic",
			expr:     NewNotExpr(UseExpr{Flag: "extended"}),
			expected: "NOT(Use(extended))",
			shell:    "! use extended",
		},
		{
			name:     "AND(A, OR(B, C))",
			expr:     NewAndExpr(a, NewOrExpr(b, c)),
			expected: "AND(OR(Use(b), Use(c)), Use(a))",
			shell:    "{ use b || use c; } && use a",
		},
		{
			name:     "AND(A, OR(B, C)) direct struct",
			expr:     AndExpr{Exprs: []conditionExpr{a, OrExpr{Exprs: []conditionExpr{b, c}}}},
			expected: "AND(Use(a), OR(Use(b), Use(c)))",
			shell:    "use a && { use b || use c; }",
		},
		{
			name:     "OR(A, AND(B, C))",
			expr:     OrExpr{Exprs: []conditionExpr{a, AndExpr{Exprs: []conditionExpr{b, c}}}},
			expected: "OR(Use(a), AND(Use(b), Use(c)))",
			shell:    "use a || { use b && use c; }",
		},
		{
			name:     "NOT(AND(A, B))",
			expr:     NotExpr{Expr: AndExpr{Exprs: []conditionExpr{a, b}}},
			expected: "NOT(AND(Use(a), Use(b)))",
			shell:    "! { use a && use b; }",
		},
		{
			name:     "NOT(OR(A, B))",
			expr:     NotExpr{Expr: OrExpr{Exprs: []conditionExpr{a, b}}},
			expected: "NOT(OR(Use(a), Use(b)))",
			shell:    "! { use a || use b; }",
		},
		{
			name:     "AND(A, NOT(OR(B, C)))",
			expr:     AndExpr{Exprs: []conditionExpr{a, NotExpr{Expr: OrExpr{Exprs: []conditionExpr{b, c}}}}},
			expected: "AND(Use(a), NOT(OR(Use(b), Use(c))))",
			shell:    "use a && ! { use b || use c; }",
		},
		{
			name: "nested combinations several levels deep",
			expr: AndExpr{
				Exprs: []conditionExpr{
					a,
					OrExpr{
						Exprs: []conditionExpr{
							b,
							AndExpr{
								Exprs: []conditionExpr{c, d},
							},
						},
					},
				},
			},
			expected: "AND(Use(a), OR(Use(b), AND(Use(c), Use(d))))",
			shell:    "use a && { use b || { use c && use d; }; }",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.expr.String())
			require.Equal(t, tc.shell, tc.expr.Shell())
		})
	}
}

func TestConditionExprAlgebra(t *testing.T) {
	t.Run("double negation", func(t *testing.T) {
		inner := UseExpr{Flag: "foo"}
		not := NewNotExpr(inner)
		require.Equal(t, "NOT(Use(foo))", not.String())
		notNot := NewNotExpr(not)
		require.Equal(t, "Use(foo)", notNot.String())
		require.True(t, inner.Equals(notNot))
	})

	t.Run("AND flattening and deduplication", func(t *testing.T) {
		a := ArchExpr{Arch: "amd64"}
		b := UseExpr{Flag: "extended"}
		c := UseExpr{Flag: "foo"}

		and1 := NewAndExpr(a, b)
		and2 := NewAndExpr(and1, c, a) // flattened & deduplicated
		require.Equal(t, "AND(Arch(amd64), Use(extended), Use(foo))", and2.String())
	})

	t.Run("OR flattening and deduplication", func(t *testing.T) {
		a := ArchExpr{Arch: "amd64"}
		b := ArchExpr{Arch: "arm64"}

		or1 := NewOrExpr(a, b)
		or2 := NewOrExpr(or1, a)
		require.Equal(t, "OR(Arch(amd64), Arch(arm64))", or2.String())
	})

	t.Run("Single element simplification", func(t *testing.T) {
		a := ArchExpr{Arch: "amd64"}
		require.Equal(t, a, NewAndExpr(a))
		require.Equal(t, a, NewOrExpr(a))
		require.Nil(t, NewAndExpr())
		require.Nil(t, NewOrExpr())
		require.Nil(t, NewNotExpr(nil))
	})
}

func TestUniversalArchSimplification(t *testing.T) {
	universe := []string{"amd64", "arm64"}

	t.Run("Universal Arch at root", func(t *testing.T) {
		or := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		require.True(t, isUniversalArchExpr(or, universe))
		require.Nil(t, simplifyExpr(or, universe))
	})

	t.Run("Single-arch universe", func(t *testing.T) {
		singleUniverse := []string{"amd64"}
		arch := ArchExpr{Arch: "amd64"}
		require.True(t, isUniversalArchExpr(arch, singleUniverse))
		require.Nil(t, simplifyExpr(arch, singleUniverse))
	})

	t.Run("Partial arch set is not universal", func(t *testing.T) {
		arch := ArchExpr{Arch: "amd64"}
		require.False(t, isUniversalArchExpr(arch, universe))
		require.Equal(t, arch, simplifyExpr(arch, universe))
	})

	t.Run("AND(UniversalArch, Use(foo)) simplifies to Use(foo)", func(t *testing.T) {
		univ := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		foo := UseExpr{Flag: "foo"}
		and := NewAndExpr(univ, foo)
		simplified := simplifyExpr(and, universe)
		require.Equal(t, foo, simplified)
	})

	t.Run("OR(UniversalArch, Use(foo)) simplifies to nil (true)", func(t *testing.T) {
		univ := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		foo := UseExpr{Flag: "foo"}
		or := NewOrExpr(univ, foo)
		simplified := simplifyExpr(or, universe)
		require.Nil(t, simplified)
	})

	t.Run("nested AND containing UniversalArch", func(t *testing.T) {
		univ := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		foo := UseExpr{Flag: "foo"}
		bar := UseExpr{Flag: "bar"}
		nested := NewAndExpr(foo, NewAndExpr(univ, bar))
		simplified := simplifyExpr(nested, universe)
		require.Equal(t, NewAndExpr(foo, bar), simplified)
	})

	t.Run("reducer unwraps universal arch condition block", func(t *testing.T) {
		plan := &installPlan{
			UniverseArchitectures: universe,
			Body: []installStmt{
				conditionStmt{
					Expr: NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"}),
					Body: []installStmt{
						actionStmt{Op: OpDoexe, Source: "app"},
					},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 1)
		require.IsType(t, actionStmt{}, reduced.Body[0])
		require.Equal(t, "doexe \"app\"\n", reduced.Body[0].String(""))
	})

	t.Run("reducer retains partial arch condition block", func(t *testing.T) {
		plan := &installPlan{
			UniverseArchitectures: universe,
			Body: []installStmt{
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						actionStmt{Op: OpDoexe, Source: "app"},
					},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 1)
		require.IsType(t, conditionStmt{}, reduced.Body[0])
	})
}

func TestSemanticInstallOperations(t *testing.T) {
	testCases := []struct {
		name     string
		stmt     actionStmt
		expected string
	}{
		{
			name:     "doexe",
			stmt:     actionStmt{Op: OpDoexe, Source: "prog"},
			expected: "doexe \"prog\"\n",
		},
		{
			name:     "newexe",
			stmt:     actionStmt{Op: OpNewexe, Source: "prog_x86", Target: "prog", Die: "Failed to install prog"},
			expected: "newexe \"prog_x86\" \"prog\" || die \"Failed to install prog\"\n",
		},
		{
			name:     "doins",
			stmt:     actionStmt{Op: OpDoins, Source: "config.yaml"},
			expected: "doins \"config.yaml\"\n",
		},
		{
			name:     "newins",
			stmt:     actionStmt{Op: OpNewins, Source: "config.example", Target: "config.yaml"},
			expected: "newins \"config.example\" \"config.yaml\"\n",
		},
		{
			name:     "dobin",
			stmt:     actionStmt{Op: OpDobin, Source: "bin/cli"},
			expected: "dobin \"bin/cli\"\n",
		},
		{
			name:     "newbin",
			stmt:     actionStmt{Op: OpNewbin, Source: "bin/cli_v2", Target: "cli"},
			expected: "newbin \"bin/cli_v2\" \"cli\"\n",
		},
		{
			name:     "dosbin",
			stmt:     actionStmt{Op: OpDosbin, Source: "sbin/daemon"},
			expected: "dosbin \"sbin/daemon\"\n",
		},
		{
			name:     "newsbin",
			stmt:     actionStmt{Op: OpNewsbin, Source: "sbin/daemon_v2", Target: "daemon"},
			expected: "newsbin \"sbin/daemon_v2\" \"daemon\"\n",
		},
		{
			name:     "doconfd",
			stmt:     actionStmt{Op: OpDoconfd, Source: "foo.confd"},
			expected: "doconfd \"foo.confd\"\n",
		},
		{
			name:     "newconfd",
			stmt:     actionStmt{Op: OpNewconfd, Source: "foo.confd", Target: "foo"},
			expected: "newconfd \"foo.confd\" \"foo\"\n",
		},
		{
			name:     "doenvd",
			stmt:     actionStmt{Op: OpDoenvd, Source: "99foo"},
			expected: "doenvd \"99foo\"\n",
		},
		{
			name:     "newenvd",
			stmt:     actionStmt{Op: OpNewenvd, Source: "99foo", Target: "99foo_renamed"},
			expected: "newenvd \"99foo\" \"99foo_renamed\"\n",
		},
		{
			name:     "doheader",
			stmt:     actionStmt{Op: OpDoheader, Source: "foo.h"},
			expected: "doheader \"foo.h\"\n",
		},
		{
			name:     "newheader",
			stmt:     actionStmt{Op: OpNewheader, Source: "foo_impl.h", Target: "foo.h"},
			expected: "newheader \"foo_impl.h\" \"foo.h\"\n",
		},
		{
			name:     "doinitd",
			stmt:     actionStmt{Op: OpDoinitd, Source: "foo.initd"},
			expected: "doinitd \"foo.initd\"\n",
		},
		{
			name:     "newinitd",
			stmt:     actionStmt{Op: OpNewinitd, Source: "foo.initd", Target: "foo"},
			expected: "newinitd \"foo.initd\" \"foo\"\n",
		},
		{
			name:     "systemd_dounit",
			stmt:     actionStmt{Op: OpSystemdDounit, Source: "foo.service"},
			expected: "systemd_dounit \"foo.service\"\n",
		},
		{
			name:     "systemd_newunit argument shape",
			stmt:     actionStmt{Op: OpSystemdNewunit, Source: "foo.service", Target: "bar.service", Die: "Failed to install unit"},
			expected: "systemd_newunit \"foo.service\" \"bar.service\" || die \"Failed to install unit\"\n",
		},
		{
			name:     "dosym",
			stmt:     actionStmt{Op: OpDosym, Source: "target", Target: "linkpath", Die: "Failed to create symlink"},
			expected: "dosym \"target\" \"linkpath\" || die \"Failed to create symlink\"\n",
		},
		{
			name:     "dodoc",
			stmt:     actionStmt{Op: OpDodoc, Source: "README.md"},
			expected: "dodoc \"README.md\"\n",
		},
		{
			name:     "newdoc",
			stmt:     actionStmt{Op: OpNewdoc, Source: "README.txt", Target: "README"},
			expected: "newdoc \"README.txt\" \"README\"\n",
		},
		{
			name:     "doman",
			stmt:     actionStmt{Op: OpDoman, Source: "foo.1"},
			expected: "doman \"foo.1\"\n",
		},
		{
			name:     "newman",
			stmt:     actionStmt{Op: OpNewman, Source: "foo_man.1", Target: "foo.1"},
			expected: "newman \"foo_man.1\" \"foo.1\"\n",
		},
		{
			name:     "dodir",
			stmt:     actionStmt{Op: OpDodir, Source: "/var/lib/myapp"},
			expected: "dodir \"/var/lib/myapp\"\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.stmt.String(""))
			require.NoError(t, tc.stmt.Validate())
		})
	}
}

func TestDestinationStateModeling(t *testing.T) {
	t.Run("same state repeated is deduplicated", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyExe, Value: "/usr/bin"},
				actionStmt{Op: OpDoexe, Source: "app1"},
				stateStmt{Family: StateFamilyExe, Value: "/usr/bin"},
				actionStmt{Op: OpDoexe, Source: "app2"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 3)
		require.Equal(t, stateStmt{Family: StateFamilyExe, Value: "/usr/bin"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDoexe, Source: "app1"}, reduced.Body[1])
		require.Equal(t, actionStmt{Op: OpDoexe, Source: "app2"}, reduced.Body[2])
	})

	t.Run("state change is retained", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyExe, Value: "/usr/bin"},
				actionStmt{Op: OpDoexe, Source: "app1"},
				stateStmt{Family: StateFamilyExe, Value: "/opt/bin"},
				actionStmt{Op: OpDoexe, Source: "app2"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 4)
	})

	t.Run("state inside conditional is local and does not eliminate required state outside", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				conditionStmt{
					Expr: UseExpr{Flag: "custom"},
					Body: []installStmt{
						stateStmt{Family: StateFamilyExe, Value: "/opt/bin"},
						actionStmt{Op: OpDoexe, Source: "app_custom"},
					},
				},
				stateStmt{Family: StateFamilyExe, Value: "/opt/bin"},
				actionStmt{Op: OpDoexe, Source: "app_main"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 3)
		require.IsType(t, conditionStmt{}, reduced.Body[0])
		require.Equal(t, stateStmt{Family: StateFamilyExe, Value: "/opt/bin"}, reduced.Body[1])
		require.Equal(t, actionStmt{Op: OpDoexe, Source: "app_main"}, reduced.Body[2])
	})

	t.Run("state families are independent (exeinto vs insinto vs into)", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyExe, Value: "/usr/bin"},
				stateStmt{Family: StateFamilyIns, Value: "/etc/myapp"},
				stateStmt{Family: StateFamilyBin, Value: "/usr"},
				actionStmt{Op: OpDoexe, Source: "app"},
				actionStmt{Op: OpDoins, Source: "config.yaml"},
				actionStmt{Op: OpDobin, Source: "tool"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 6)
	})
}

func TestRawStatementRendering(t *testing.T) {
	t.Run("single raw line", func(t *testing.T) {
		stmt := rawStmt{Content: "echo \"hello\""}
		require.Equal(t, "  echo \"hello\"\n", stmt.String("  "))
	})

	t.Run("multiline raw with relative indentation", func(t *testing.T) {
		stmt := rawStmt{Content: "if foo; then\n    bar\nfi"}
		expected := "  if foo; then\n      bar\n  fi\n"
		require.Equal(t, expected, stmt.String("  "))
	})

	t.Run("blank line and trailing newline", func(t *testing.T) {
		stmt := rawStmt{Content: "line1\n\nline2\n"}
		expected := "  line1\n\n  line2\n\n"
		require.Equal(t, expected, stmt.String("  "))
	})

	t.Run("raw nested inside condition with custom indent", func(t *testing.T) {
		cond := conditionStmt{
			Expr: UseExpr{Flag: "foo"},
			Body: []installStmt{
				rawStmt{Content: "setup_env\nrun_setup"},
			},
		}
		expected := ">>>>if use foo; then\n>>>>  setup_env\n>>>>  run_setup\n>>>>fi\n"
		require.Equal(t, expected, cond.String(">>>>"))
	})
}

func TestInitialIndentationInput(t *testing.T) {
	plan := &installPlan{
		Body: []installStmt{
			conditionStmt{
				Expr: UseExpr{Flag: "foo"},
				Body: []installStmt{
					conditionStmt{
						Expr: UseExpr{Flag: "bar"},
						Body: []installStmt{
							actionStmt{Op: OpDoexe, Source: "app"},
						},
					},
				},
			},
		},
	}
	rendered := plan.String(">>>>")
	expected := ">>>>if use foo; then\n>>>>  if use bar; then\n>>>>    doexe \"app\"\n>>>>  fi\n>>>>fi"
	require.Equal(t, expected, rendered)
}

func TestFactoringPreservesStatementOrdering(t *testing.T) {
	// Condition X -> A
	// Raw -> R
	// Condition X -> B
	// Must NOT factor A and B together across R!
	plan := &installPlan{
		Body: []installStmt{
			conditionStmt{
				Expr: UseExpr{Flag: "x"},
				Body: []installStmt{
					actionStmt{Op: OpDoexe, Source: "a"},
				},
			},
			rawStmt{Content: "echo \"step in between\""},
			conditionStmt{
				Expr: UseExpr{Flag: "x"},
				Body: []installStmt{
					actionStmt{Op: OpDoexe, Source: "b"},
				},
			},
		},
	}
	reduced := plan.reducePlan()
	require.Len(t, reduced.Body, 3)
	require.IsType(t, conditionStmt{}, reduced.Body[0])
	require.IsType(t, rawStmt{}, reduced.Body[1])
	require.IsType(t, conditionStmt{}, reduced.Body[2])
}

func TestSiblingConditionMergingAndFactoring(t *testing.T) {
	t.Run("exact sibling conditions merged", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						actionStmt{Op: OpDoexe, Source: "app1"},
					},
				},
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						actionStmt{Op: OpDoexe, Source: "app2"},
					},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 1)
		cond := reduced.Body[0].(conditionStmt)
		require.Equal(t, ArchExpr{Arch: "amd64"}, cond.Expr)
		require.Len(t, cond.Body, 2)
	})

	t.Run("factoring common architecture with nested positive and negative USE", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				conditionStmt{
					Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, UseExpr{Flag: "extended"}),
					Body: []installStmt{
						actionStmt{Op: OpDoexe, Source: "app_ext"},
					},
				},
				conditionStmt{
					Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, NewNotExpr(UseExpr{Flag: "extended"})),
					Body: []installStmt{
						actionStmt{Op: OpDoexe, Source: "app_std"},
					},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 1)
		cond := reduced.Body[0].(conditionStmt)
		require.Equal(t, ArchExpr{Arch: "amd64"}, cond.Expr)
		require.Len(t, cond.Body, 2)
		require.Equal(t, UseExpr{Flag: "extended"}, cond.Body[0].(conditionStmt).Expr)
		require.Equal(t, NewNotExpr(UseExpr{Flag: "extended"}), cond.Body[1].(conditionStmt).Expr)
	})

	t.Run("multi-level recursive factoring", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				conditionStmt{
					Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, UseExpr{Flag: "gui"}, UseExpr{Flag: "opengl"}),
					Body: []installStmt{actionStmt{Op: OpDoexe, Source: "app_gui_gl"}},
				},
				conditionStmt{
					Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, UseExpr{Flag: "gui"}, NewNotExpr(UseExpr{Flag: "opengl"})),
					Body: []installStmt{actionStmt{Op: OpDoexe, Source: "app_gui_sw"}},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 1)
		outer := reduced.Body[0].(conditionStmt)
		require.Equal(t, ArchExpr{Arch: "amd64"}, outer.Expr)
		require.Len(t, outer.Body, 1)
		mid := outer.Body[0].(conditionStmt)
		require.Equal(t, UseExpr{Flag: "gui"}, mid.Expr)
		require.Len(t, mid.Body, 2)
	})
}

func TestReducerIdempotenceAndConvergence(t *testing.T) {
	plan := &installPlan{
		UniverseArchitectures: []string{"amd64", "arm64", "riscv64"},
		Body: []installStmt{
			stateStmt{Family: StateFamilyExe, Value: "/usr/bin"},
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, UseExpr{Flag: "extended"}),
				Body: []installStmt{actionStmt{Op: OpDoexe, Source: "prog_amd64_ext"}},
			},
			conditionStmt{
				Expr: NewAndExpr(ArchExpr{Arch: "amd64"}, NewNotExpr(UseExpr{Flag: "extended"})),
				Body: []installStmt{actionStmt{Op: OpDoexe, Source: "prog_amd64_std"}},
			},
			conditionStmt{
				Expr: ArchExpr{Arch: "arm64"},
				Body: []installStmt{actionStmt{Op: OpDoexe, Source: "prog_arm64"}},
			},
		},
	}

	once := plan.reducePlan()
	twice := once.reducePlan()

	require.True(t, planEqual(once, twice))
	require.Equal(t, once, twice)
}

func TestRecursiveValidation(t *testing.T) {
	t.Run("valid plan", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyExe, Value: "/usr/bin"},
				actionStmt{Op: OpDoexe, Source: "app"},
				actionStmt{Op: OpDosym, Source: "target", Target: "link"},
				actionStmt{Op: OpNewexe, Source: "app_x86", Target: "app"},
			},
		}
		require.NoError(t, plan.Validate())
	})

	t.Run("invalid condition without expr", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				conditionStmt{
					Expr: nil,
					Body: []installStmt{actionStmt{Op: OpDoexe, Source: "app"}},
				},
			},
		}
		require.Error(t, plan.Validate())
	})

	t.Run("invalid dosym without destination", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{Op: OpDosym, Source: "app"},
			},
		}
		require.EqualError(t, plan.Validate(), "dosym requires a destination")
	})

	t.Run("invalid rename without destination", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{Op: OpNewexe, Source: "app"},
			},
		}
		require.EqualError(t, plan.Validate(), "newexe requires a destination")
	})

	t.Run("invalid state without family", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: "", Value: "/usr/bin"},
			},
		}
		require.EqualError(t, plan.Validate(), "stateStmt requires a state family")
	})

	t.Run("nil plan validate is nil", func(t *testing.T) {
		var plan *installPlan
		require.NoError(t, plan.Validate())
	})
}
