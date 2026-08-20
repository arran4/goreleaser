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

	t.Run("double NOT involving True and False", func(t *testing.T) {
		tr := TrueExpr{}
		fa := FalseExpr{}

		require.Equal(t, fa, NewNotExpr(tr))
		require.Equal(t, tr, NewNotExpr(NewNotExpr(tr)))
		require.Equal(t, tr, NewNotExpr(fa))
		require.Equal(t, fa, NewNotExpr(NewNotExpr(fa)))
	})

	t.Run("Boolean constant identities", func(t *testing.T) {
		x := UseExpr{Flag: "x"}

		// NOT(True) -> False, NOT(False) -> True
		require.Equal(t, FalseExpr{}, NewNotExpr(TrueExpr{}))
		require.Equal(t, TrueExpr{}, NewNotExpr(FalseExpr{}))

		// AND(True, X) -> X, AND(False, X) -> False
		require.Equal(t, x, NewAndExpr(TrueExpr{}, x))
		require.Equal(t, FalseExpr{}, NewAndExpr(FalseExpr{}, x))

		// OR(True, X) -> True, OR(False, X) -> X
		require.Equal(t, TrueExpr{}, NewOrExpr(TrueExpr{}, x))
		require.Equal(t, x, NewOrExpr(FalseExpr{}, x))
	})

	t.Run("nested True/False several levels deep", func(t *testing.T) {
		foo := UseExpr{Flag: "foo"}

		// AND(True, OR(False, AND(True, Use(foo)))) -> Use(foo)
		nested1 := NewAndExpr(TrueExpr{}, NewOrExpr(FalseExpr{}, NewAndExpr(TrueExpr{}, foo)))
		require.Equal(t, foo, nested1)

		// OR(False, AND(True, True)) -> True
		nested2 := NewOrExpr(FalseExpr{}, NewAndExpr(TrueExpr{}, TrueExpr{}))
		require.Equal(t, TrueExpr{}, nested2)

		// NOT(AND(True, NOT(False))) -> False
		nested3 := NewNotExpr(NewAndExpr(TrueExpr{}, NewNotExpr(FalseExpr{})))
		require.Equal(t, FalseExpr{}, nested3)

		// AND(Use(a), OR(True, AND(False, Use(b)))) -> Use(a)
		a := UseExpr{Flag: "a"}
		b := UseExpr{Flag: "b"}
		nested4 := NewAndExpr(a, NewOrExpr(TrueExpr{}, NewAndExpr(FalseExpr{}, b)))
		require.Equal(t, a, nested4)
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

	t.Run("Universal Arch at root yields True", func(t *testing.T) {
		or := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		require.True(t, isUniversalArchExpr(or, universe))
		require.Equal(t, TrueExpr{}, simplifyExpr(or, universe))
	})

	t.Run("Single-arch universe yields True", func(t *testing.T) {
		singleUniverse := []string{"amd64"}
		arch := ArchExpr{Arch: "amd64"}
		require.True(t, isUniversalArchExpr(arch, singleUniverse))
		require.Equal(t, TrueExpr{}, simplifyExpr(arch, singleUniverse))
	})

	t.Run("Partial arch set is not universal", func(t *testing.T) {
		arch := ArchExpr{Arch: "amd64"}
		require.False(t, isUniversalArchExpr(arch, universe))
		require.Equal(t, arch, simplifyExpr(arch, universe))
	})

	t.Run("NOT(universal architecture) simplifies to False", func(t *testing.T) {
		univ := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		notUniv := NewNotExpr(univ)
		simplified := simplifyExpr(notUniv, universe)
		require.Equal(t, FalseExpr{}, simplified)
	})

	t.Run("AND(NOT(universal architecture), Use(foo)) simplifies to False", func(t *testing.T) {
		univ := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		notUniv := NewNotExpr(univ)
		foo := UseExpr{Flag: "foo"}
		and := NewAndExpr(notUniv, foo)
		simplified := simplifyExpr(and, universe)
		require.Equal(t, FalseExpr{}, simplified)
	})

	t.Run("OR(NOT(universal architecture), Use(foo)) simplifies to Use(foo)", func(t *testing.T) {
		univ := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		notUniv := NewNotExpr(univ)
		foo := UseExpr{Flag: "foo"}
		or := NewOrExpr(notUniv, foo)
		simplified := simplifyExpr(or, universe)
		require.Equal(t, foo, simplified)
	})

	t.Run("AND(UniversalArch, Use(foo)) simplifies to Use(foo)", func(t *testing.T) {
		univ := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		foo := UseExpr{Flag: "foo"}
		and := NewAndExpr(univ, foo)
		simplified := simplifyExpr(and, universe)
		require.Equal(t, foo, simplified)
	})

	t.Run("OR(UniversalArch, Use(foo)) simplifies to True", func(t *testing.T) {
		univ := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		foo := UseExpr{Flag: "foo"}
		or := NewOrExpr(univ, foo)
		simplified := simplifyExpr(or, universe)
		require.Equal(t, TrueExpr{}, simplified)
	})

	t.Run("nested AND containing UniversalArch", func(t *testing.T) {
		univ := NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})
		foo := UseExpr{Flag: "foo"}
		bar := UseExpr{Flag: "bar"}
		nested := NewAndExpr(foo, NewAndExpr(univ, bar))
		simplified := simplifyExpr(nested, universe)
		require.Equal(t, NewAndExpr(foo, bar), simplified)
	})

	t.Run("reducer unwraps universal arch condition block (True)", func(t *testing.T) {
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
		require.Equal(t, "doexe \"app\" || die \"Failed to install app\"\n", reduced.Body[0].String(""))
	})

	t.Run("reducer removes negated universal arch condition block (False)", func(t *testing.T) {
		plan := &installPlan{
			UniverseArchitectures: universe,
			Body: []installStmt{
				conditionStmt{
					Expr: NewNotExpr(NewOrExpr(ArchExpr{Arch: "amd64"}, ArchExpr{Arch: "arm64"})),
					Body: []installStmt{
						actionStmt{Op: OpDoexe, Source: "app"},
					},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Empty(t, reduced.Body)
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
			expected: "doexe \"prog\" || die \"Failed to install prog\"\n",
		},
		{
			name:     "newexe",
			stmt:     actionStmt{Op: OpNewexe, Source: "prog_x86", Target: "prog", Die: "Failed to install prog"},
			expected: "newexe \"prog_x86\" \"prog\" || die \"Failed to install prog\"\n",
		},
		{
			name:     "doins",
			stmt:     actionStmt{Op: OpDoins, Source: "config.yaml"},
			expected: "doins \"config.yaml\" || die \"Failed to install config.yaml\"\n",
		},
		{
			name:     "newins",
			stmt:     actionStmt{Op: OpNewins, Source: "config.example", Target: "config.yaml"},
			expected: "newins \"config.example\" \"config.yaml\" || die \"Failed to install config.yaml\"\n",
		},
		{
			name:     "dobin",
			stmt:     actionStmt{Op: OpDobin, Source: "bin/cli"},
			expected: "dobin \"bin/cli\" || die \"Failed to install bin/cli\"\n",
		},
		{
			name:     "newbin",
			stmt:     actionStmt{Op: OpNewbin, Source: "bin/cli_v2", Target: "cli"},
			expected: "newbin \"bin/cli_v2\" \"cli\" || die \"Failed to install cli\"\n",
		},
		{
			name:     "dosbin",
			stmt:     actionStmt{Op: OpDosbin, Source: "sbin/daemon"},
			expected: "dosbin \"sbin/daemon\" || die \"Failed to install sbin/daemon\"\n",
		},
		{
			name:     "newsbin",
			stmt:     actionStmt{Op: OpNewsbin, Source: "sbin/daemon_v2", Target: "daemon"},
			expected: "newsbin \"sbin/daemon_v2\" \"daemon\" || die \"Failed to install daemon\"\n",
		},
		{
			name:     "doconfd",
			stmt:     actionStmt{Op: OpDoconfd, Source: "foo.confd"},
			expected: "doconfd \"foo.confd\" || die \"Failed to install foo.confd\"\n",
		},
		{
			name:     "newconfd",
			stmt:     actionStmt{Op: OpNewconfd, Source: "foo.confd", Target: "foo"},
			expected: "newconfd \"foo.confd\" \"foo\" || die \"Failed to install foo\"\n",
		},
		{
			name:     "doenvd",
			stmt:     actionStmt{Op: OpDoenvd, Source: "99foo"},
			expected: "doenvd \"99foo\" || die \"Failed to install 99foo\"\n",
		},
		{
			name:     "newenvd",
			stmt:     actionStmt{Op: OpNewenvd, Source: "99foo", Target: "99foo_renamed"},
			expected: "newenvd \"99foo\" \"99foo_renamed\" || die \"Failed to install 99foo_renamed\"\n",
		},
		{
			name:     "doheader",
			stmt:     actionStmt{Op: OpDoheader, Source: "foo.h"},
			expected: "doheader \"foo.h\" || die \"Failed to install foo.h\"\n",
		},
		{
			name:     "newheader",
			stmt:     actionStmt{Op: OpNewheader, Source: "foo_impl.h", Target: "foo.h"},
			expected: "newheader \"foo_impl.h\" \"foo.h\" || die \"Failed to install foo.h\"\n",
		},
		{
			name:     "doinitd",
			stmt:     actionStmt{Op: OpDoinitd, Source: "foo.initd"},
			expected: "doinitd \"foo.initd\" || die \"Failed to install foo.initd\"\n",
		},
		{
			name:     "newinitd",
			stmt:     actionStmt{Op: OpNewinitd, Source: "foo.initd", Target: "foo"},
			expected: "newinitd \"foo.initd\" \"foo\" || die \"Failed to install foo\"\n",
		},
		{
			name:     "systemd_dounit",
			stmt:     actionStmt{Op: OpSystemdDounit, Source: "foo.service"},
			expected: "systemd_dounit \"foo.service\" || die \"Failed to install foo.service\"\n",
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

	t.Run("first required setter is retained when no initial state", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyExe, Value: "/opt/bin"},
				actionStmt{Op: OpDoexe, Source: "foo"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 2)
		require.Equal(t, stateStmt{Family: StateFamilyExe, Value: "/opt/bin"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDoexe, Source: "foo"}, reduced.Body[1])
	})

	t.Run("default initial state is suppressed at plan start", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyBin, Value: "/usr"},
				actionStmt{Op: OpDobin, Source: "bin1"},
				stateStmt{Family: StateFamilyDoc, Value: ""},
				actionStmt{Op: OpDodoc, Source: "README.md"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 2)
		require.Equal(t, actionStmt{Op: OpDobin, Source: "bin1"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDodoc, Source: "README.md"}, reduced.Body[1])
	})

	t.Run("consecutive identical setters are deduplicated", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyExe, Value: "/opt/bin"},
				stateStmt{Family: StateFamilyExe, Value: "/opt/bin"},
				actionStmt{Op: OpDoexe, Source: "foo"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 2)
		require.Equal(t, stateStmt{Family: StateFamilyExe, Value: "/opt/bin"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDoexe, Source: "foo"}, reduced.Body[1])
	})

	t.Run("conditional dataflow restores required state outside branch", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyExe, Value: "/a"},
				actionStmt{Op: OpDoexe, Source: "a"},
				conditionStmt{
					Expr: UseExpr{Flag: "x"},
					Body: []installStmt{
						stateStmt{Family: StateFamilyExe, Value: "/b"},
						actionStmt{Op: OpDoexe, Source: "x"},
					},
				},
				stateStmt{Family: StateFamilyExe, Value: "/a"},
				actionStmt{Op: OpDoexe, Source: "y"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 5)
		require.Equal(t, stateStmt{Family: StateFamilyExe, Value: "/a"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDoexe, Source: "a"}, reduced.Body[1])
		require.IsType(t, conditionStmt{}, reduced.Body[2])
		require.Equal(t, stateStmt{Family: StateFamilyExe, Value: "/a"}, reduced.Body[3])
		require.Equal(t, actionStmt{Op: OpDoexe, Source: "y"}, reduced.Body[4])
	})

	t.Run("insinto retention and deduplication", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyIns, Value: "/etc/myapp"},
				actionStmt{Op: OpDoins, Source: "config.yaml"},
				stateStmt{Family: StateFamilyIns, Value: "/etc/myapp"},
				actionStmt{Op: OpDoins, Source: "extra.yaml"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 3)
		require.Equal(t, stateStmt{Family: StateFamilyIns, Value: "/etc/myapp"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDoins, Source: "config.yaml"}, reduced.Body[1])
		require.Equal(t, actionStmt{Op: OpDoins, Source: "extra.yaml"}, reduced.Body[2])
	})

	t.Run("into and docinto retention and deduplication", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				stateStmt{Family: StateFamilyBin, Value: "/usr/local"},
				actionStmt{Op: OpDobin, Source: "tool1"},
				stateStmt{Family: StateFamilyBin, Value: "/usr/local"},
				actionStmt{Op: OpDobin, Source: "tool2"},
				stateStmt{Family: StateFamilyDoc, Value: "html"},
				actionStmt{Op: OpDodoc, Source: "index.html"},
				stateStmt{Family: StateFamilyDoc, Value: "html"},
				actionStmt{Op: OpDodoc, Source: "style.css"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 6)
		require.Equal(t, stateStmt{Family: StateFamilyBin, Value: "/usr/local"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDobin, Source: "tool1"}, reduced.Body[1])
		require.Equal(t, actionStmt{Op: OpDobin, Source: "tool2"}, reduced.Body[2])
		require.Equal(t, stateStmt{Family: StateFamilyDoc, Value: "html"}, reduced.Body[3])
		require.Equal(t, actionStmt{Op: OpDodoc, Source: "index.html"}, reduced.Body[4])
		require.Equal(t, actionStmt{Op: OpDodoc, Source: "style.css"}, reduced.Body[5])
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
				stateStmt{Family: StateFamilyBin, Value: "/usr/local"},
				actionStmt{Op: OpDoexe, Source: "app"},
				actionStmt{Op: OpDoins, Source: "config.yaml"},
				actionStmt{Op: OpDobin, Source: "tool"},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 6)
	})
}

func TestStateFamilyDescriptor(t *testing.T) {
	testCases := []struct {
		family                  StateFamily
		expectedCommand         string
		expectedHasKnownInitial bool
		expectedDefaultState    string
		expectedRequiresInit    bool
	}{
		{
			family:                  StateFamilyExe,
			expectedCommand:         "exeinto",
			expectedHasKnownInitial: false,
			expectedDefaultState:    "",
			expectedRequiresInit:    true,
		},
		{
			family:                  StateFamilyIns,
			expectedCommand:         "insinto",
			expectedHasKnownInitial: false,
			expectedDefaultState:    "/",
			expectedRequiresInit:    true,
		},
		{
			family:                  StateFamilyBin,
			expectedCommand:         "into",
			expectedHasKnownInitial: true,
			expectedDefaultState:    "/usr",
			expectedRequiresInit:    false,
		},
		{
			family:                  StateFamilyDoc,
			expectedCommand:         "docinto",
			expectedHasKnownInitial: true,
			expectedDefaultState:    "",
			expectedRequiresInit:    false,
		},
		{
			family:                  StateFamilyNone,
			expectedCommand:         "",
			expectedHasKnownInitial: false,
			expectedDefaultState:    "",
			expectedRequiresInit:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(string(tc.family), func(t *testing.T) {
			desc := tc.family.Descriptor()
			require.Equal(t, tc.expectedCommand, desc.Command)
			require.Equal(t, tc.expectedHasKnownInitial, desc.HasKnownInitialState)
			require.Equal(t, tc.expectedDefaultState, desc.DefaultState)
			require.Equal(t, tc.expectedRequiresInit, desc.RequiresStateInitialization)
		})
	}

	t.Run("InitialState map", func(t *testing.T) {
		initial := InitialState()
		require.Equal(t, "/usr", initial[StateFamilyBin])
		require.Empty(t, initial[StateFamilyDoc])
		_, hasExe := initial[StateFamilyExe]
		require.False(t, hasExe)
		_, hasIns := initial[StateFamilyIns]
		require.False(t, hasIns)
	})
}

func TestOpDescriptors(t *testing.T) {
	testCases := []struct {
		op                      InstallOp
		expectedCommand         string
		expectedArgMode         ArgMode
		expectedSupportsRename  bool
		expectedRenameOp        InstallOp
		expectedIsRename        bool
		expectedStateFamily     StateFamily
		expectedRequiresInit    bool
		expectedDefaultState    string
		expectedHasKnownInitial bool
		expectedDestMode        DestinationMode
		expectedAppendDie       bool
	}{
		{
			op:                     OpDoexe,
			expectedCommand:        "doexe",
			expectedArgMode:        ArgModeSingle,
			expectedSupportsRename: true,
			expectedRenameOp:       OpNewexe,
			expectedStateFamily:    StateFamilyExe,
			expectedRequiresInit:   true,
			expectedDestMode:       DestinationModeExe,
			expectedAppendDie:      true,
		},
		{
			op:                   OpNewexe,
			expectedCommand:      "newexe",
			expectedArgMode:      ArgModeRename,
			expectedIsRename:     true,
			expectedStateFamily:  StateFamilyExe,
			expectedRequiresInit: true,
			expectedDestMode:     DestinationModeExe,
			expectedAppendDie:    true,
		},
		{
			op:                      OpDobin,
			expectedCommand:         "dobin",
			expectedArgMode:         ArgModeSingle,
			expectedSupportsRename:  true,
			expectedRenameOp:        OpNewbin,
			expectedStateFamily:     StateFamilyBin,
			expectedDefaultState:    "/usr",
			expectedHasKnownInitial: true,
			expectedDestMode:        DestinationModeIntoBin,
			expectedAppendDie:       true,
		},
		{
			op:                      OpNewbin,
			expectedCommand:         "newbin",
			expectedArgMode:         ArgModeRename,
			expectedIsRename:        true,
			expectedStateFamily:     StateFamilyBin,
			expectedDefaultState:    "/usr",
			expectedHasKnownInitial: true,
			expectedDestMode:        DestinationModeIntoBin,
			expectedAppendDie:       true,
		},
		{
			op:                      OpDosbin,
			expectedCommand:         "dosbin",
			expectedArgMode:         ArgModeSingle,
			expectedSupportsRename:  true,
			expectedRenameOp:        OpNewsbin,
			expectedStateFamily:     StateFamilyBin,
			expectedDefaultState:    "/usr",
			expectedHasKnownInitial: true,
			expectedDestMode:        DestinationModeIntoSbin,
			expectedAppendDie:       true,
		},
		{
			op:                      OpNewsbin,
			expectedCommand:         "newsbin",
			expectedArgMode:         ArgModeRename,
			expectedIsRename:        true,
			expectedStateFamily:     StateFamilyBin,
			expectedDefaultState:    "/usr",
			expectedHasKnownInitial: true,
			expectedDestMode:        DestinationModeIntoSbin,
			expectedAppendDie:       true,
		},
		{
			op:                      OpDoins,
			expectedCommand:         "doins",
			expectedArgMode:         ArgModeSingle,
			expectedSupportsRename:  true,
			expectedRenameOp:        OpNewins,
			expectedStateFamily:     StateFamilyIns,
			expectedRequiresInit:    true,
			expectedDefaultState:    "/",
			expectedHasKnownInitial: false,
			expectedDestMode:        DestinationModeIns,
			expectedAppendDie:       true,
		},
		{
			op:                      OpNewins,
			expectedCommand:         "newins",
			expectedArgMode:         ArgModeRename,
			expectedIsRename:        true,
			expectedStateFamily:     StateFamilyIns,
			expectedRequiresInit:    true,
			expectedDefaultState:    "/",
			expectedHasKnownInitial: false,
			expectedDestMode:        DestinationModeIns,
			expectedAppendDie:       true,
		},
		{
			op:                     OpDoconfd,
			expectedCommand:        "doconfd",
			expectedArgMode:        ArgModeSingle,
			expectedSupportsRename: true,
			expectedRenameOp:       OpNewconfd,
			expectedDestMode:       DestinationModeFixed,
			expectedAppendDie:      true,
		},
		{
			op:                     OpSystemdDounit,
			expectedCommand:        "systemd_dounit",
			expectedArgMode:        ArgModeSingle,
			expectedSupportsRename: true,
			expectedRenameOp:       OpSystemdNewunit,
			expectedDestMode:       DestinationModeFixed,
			expectedAppendDie:      true,
		},
		{
			op:                OpDosym,
			expectedCommand:   "dosym",
			expectedArgMode:   ArgModeTwoArgs,
			expectedDestMode:  DestinationModeSymlink,
			expectedAppendDie: true,
		},
		{
			op:                      OpDodoc,
			expectedCommand:         "dodoc",
			expectedArgMode:         ArgModeSingle,
			expectedSupportsRename:  true,
			expectedRenameOp:        OpNewdoc,
			expectedStateFamily:     StateFamilyDoc,
			expectedHasKnownInitial: true,
			expectedDestMode:        DestinationModeDoc,
			expectedAppendDie:       false,
		},
		{
			op:                      OpNewdoc,
			expectedCommand:         "newdoc",
			expectedArgMode:         ArgModeRename,
			expectedIsRename:        true,
			expectedStateFamily:     StateFamilyDoc,
			expectedHasKnownInitial: true,
			expectedDestMode:        DestinationModeDoc,
			expectedAppendDie:       false,
		},
		{
			op:                     OpDoman,
			expectedCommand:        "doman",
			expectedArgMode:        ArgModeSingle,
			expectedSupportsRename: true,
			expectedRenameOp:       OpNewman,
			expectedDestMode:       DestinationModeMan,
			expectedAppendDie:      false,
		},
		{
			op:                OpNewman,
			expectedCommand:   "newman",
			expectedArgMode:   ArgModeRename,
			expectedIsRename:  true,
			expectedDestMode:  DestinationModeMan,
			expectedAppendDie: false,
		},
		{
			op:                OpDodir,
			expectedCommand:   "dodir",
			expectedArgMode:   ArgModeSingle,
			expectedDestMode:  DestinationModeDir,
			expectedAppendDie: false,
		},
	}

	for _, tc := range testCases {
		t.Run(string(tc.op), func(t *testing.T) {
			desc := tc.op.Descriptor()
			require.Equal(t, tc.expectedCommand, desc.Command)
			require.Equal(t, tc.expectedArgMode, desc.ArgMode)
			require.Equal(t, tc.expectedSupportsRename, desc.SupportsRename)
			require.Equal(t, tc.expectedRenameOp, desc.RenameOp)
			require.Equal(t, tc.expectedIsRename, desc.IsRename)
			require.Equal(t, tc.expectedStateFamily, desc.StateFamily)
			require.Equal(t, tc.expectedRequiresInit, desc.RequiresStateInitialization)
			require.Equal(t, tc.expectedDefaultState, desc.DefaultState)
			require.Equal(t, tc.expectedHasKnownInitial, desc.HasKnownInitialState)
			require.Equal(t, tc.expectedDestMode, desc.DestinationMode)
			require.Equal(t, tc.expectedAppendDie, desc.AppendDie)
		})
	}
}

func TestStateResetAndRestoration(t *testing.T) {
	t.Run("custom dobin -> default dobin", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{
					Op:            OpDobin,
					Source:        "custom",
					RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr/local"},
				},
				actionStmt{
					Op:            OpDobin,
					Source:        "normal",
					RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr"},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 4)
		require.Equal(t, stateStmt{Family: StateFamilyBin, Value: "/usr/local"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDobin, Source: "custom", RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr/local"}}, reduced.Body[1])
		require.Equal(t, stateStmt{Family: StateFamilyBin, Value: "/usr"}, reduced.Body[2])
		require.Equal(t, actionStmt{Op: OpDobin, Source: "normal", RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr"}}, reduced.Body[3])
	})

	t.Run("custom dobin -> default dosbin", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{
					Op:            OpDobin,
					Source:        "custom",
					RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr/local"},
				},
				actionStmt{
					Op:            OpDosbin,
					Source:        "normal",
					RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr"},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 4)
		require.Equal(t, stateStmt{Family: StateFamilyBin, Value: "/usr/local"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDobin, Source: "custom", RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr/local"}}, reduced.Body[1])
		require.Equal(t, stateStmt{Family: StateFamilyBin, Value: "/usr"}, reduced.Body[2])
		require.Equal(t, actionStmt{Op: OpDosbin, Source: "normal", RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr"}}, reduced.Body[3])
	})

	t.Run("custom dosbin -> default dobin", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{
					Op:            OpDosbin,
					Source:        "custom",
					RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/opt/foo"},
				},
				actionStmt{
					Op:            OpDobin,
					Source:        "normal",
					RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr"},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 4)
		require.Equal(t, stateStmt{Family: StateFamilyBin, Value: "/opt/foo"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDosbin, Source: "custom", RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/opt/foo"}}, reduced.Body[1])
		require.Equal(t, stateStmt{Family: StateFamilyBin, Value: "/usr"}, reduced.Body[2])
		require.Equal(t, actionStmt{Op: OpDobin, Source: "normal", RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr"}}, reduced.Body[3])
	})

	t.Run("custom dosbin -> default dosbin", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{
					Op:            OpDosbin,
					Source:        "custom",
					RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/opt/foo"},
				},
				actionStmt{
					Op:            OpDosbin,
					Source:        "normal",
					RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr"},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 4)
		require.Equal(t, stateStmt{Family: StateFamilyBin, Value: "/opt/foo"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDosbin, Source: "custom", RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/opt/foo"}}, reduced.Body[1])
		require.Equal(t, stateStmt{Family: StateFamilyBin, Value: "/usr"}, reduced.Body[2])
		require.Equal(t, actionStmt{Op: OpDosbin, Source: "normal", RequiredState: StateRequirement{Family: StateFamilyBin, Value: "/usr"}}, reduced.Body[3])
	})

	t.Run("custom insinto -> default doins", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{
					Op:            OpDoins,
					Source:        "custom.conf",
					RequiredState: StateRequirement{Family: StateFamilyIns, Value: "/etc/foo"},
				},
				actionStmt{
					Op:            OpDoins,
					Source:        "normal.yaml",
					RequiredState: StateRequirement{Family: StateFamilyIns, Value: "/"},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 4)
		require.Equal(t, stateStmt{Family: StateFamilyIns, Value: "/etc/foo"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDoins, Source: "custom.conf", RequiredState: StateRequirement{Family: StateFamilyIns, Value: "/etc/foo"}}, reduced.Body[1])
		require.Equal(t, stateStmt{Family: StateFamilyIns, Value: "/"}, reduced.Body[2])
		require.Equal(t, actionStmt{Op: OpDoins, Source: "normal.yaml", RequiredState: StateRequirement{Family: StateFamilyIns, Value: "/"}}, reduced.Body[3])
	})

	t.Run("custom docinto -> default dodoc", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{
					Op:            OpDodoc,
					Source:        "manual.html",
					RequiredState: StateRequirement{Family: StateFamilyDoc, Value: "html"},
				},
				actionStmt{
					Op:            OpDodoc,
					Source:        "README.md",
					RequiredState: StateRequirement{Family: StateFamilyDoc, Value: ""},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 4)
		require.Equal(t, stateStmt{Family: StateFamilyDoc, Value: "html"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDodoc, Source: "manual.html", RequiredState: StateRequirement{Family: StateFamilyDoc, Value: "html"}}, reduced.Body[1])
		require.Equal(t, stateStmt{Family: StateFamilyDoc, Value: ""}, reduced.Body[2])
		require.Equal(t, actionStmt{Op: OpDodoc, Source: "README.md", RequiredState: StateRequirement{Family: StateFamilyDoc, Value: ""}}, reduced.Body[3])
	})

	t.Run("first doexe path must explicitly execute exeinto", func(t *testing.T) {
		plan := &installPlan{
			Body: []installStmt{
				actionStmt{
					Op:            OpDoexe,
					Source:        "prog",
					RequiredState: StateRequirement{Family: StateFamilyExe, Value: "/usr/bin"},
				},
			},
		}
		reduced := plan.reducePlan()
		require.Len(t, reduced.Body, 2)
		require.Equal(t, stateStmt{Family: StateFamilyExe, Value: "/usr/bin"}, reduced.Body[0])
		require.Equal(t, actionStmt{Op: OpDoexe, Source: "prog", RequiredState: StateRequirement{Family: StateFamilyExe, Value: "/usr/bin"}}, reduced.Body[1])
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
	expected := ">>>>if use foo; then\n>>>>  if use bar; then\n>>>>    doexe \"app\" || die \"Failed to install app\"\n>>>>  fi\n>>>>fi"
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
	t.Run("canonical reduction idempotence", func(t *testing.T) {
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
	})

	t.Run("deeply nested structure idempotence", func(t *testing.T) {
		plan := &installPlan{
			UniverseArchitectures: []string{"amd64", "arm64", "riscv64"},
			Body: []installStmt{
				stateStmt{Family: StateFamilyExe, Value: "/opt/bin"},
				conditionStmt{
					Expr: ArchExpr{Arch: "amd64"},
					Body: []installStmt{
						conditionStmt{
							Expr: UseExpr{Flag: "gui"},
							Body: []installStmt{
								conditionStmt{
									Expr: UseExpr{Flag: "opengl"},
									Body: []installStmt{
										stateStmt{Family: StateFamilyExe, Value: "/opt/gl"},
										actionStmt{Op: OpDoexe, Source: "app_gl"},
									},
								},
								conditionStmt{
									Expr: NewNotExpr(UseExpr{Flag: "opengl"}),
									Body: []installStmt{
										stateStmt{Family: StateFamilyExe, Value: "/opt/sw"},
										actionStmt{Op: OpDoexe, Source: "app_sw"},
									},
								},
							},
						},
						conditionStmt{
							Expr: NewNotExpr(UseExpr{Flag: "gui"}),
							Body: []installStmt{
								actionStmt{Op: OpDoexe, Source: "app_cli"},
							},
						},
					},
				},
				conditionStmt{
					Expr: ArchExpr{Arch: "arm64"},
					Body: []installStmt{
						actionStmt{Op: OpDoexe, Source: "app_arm"},
					},
				},
				stateStmt{Family: StateFamilyExe, Value: "/opt/bin"},
				actionStmt{Op: OpDoexe, Source: "app_common"},
			},
		}

		once := plan.reducePlan()
		twice := once.reducePlan()
		thrice := twice.reducePlan()

		require.True(t, planEqual(once, twice))
		require.True(t, planEqual(twice, thrice))
		require.Equal(t, once, twice)
	})
}

func TestDecomposeDestination(t *testing.T) {
	testCases := []struct {
		name           string
		section        string
		src            string
		dst            string
		defaultDir     string
		expectedFamily StateFamily
		expectedDir    string
		expectedBase   string
		expectErr      bool
		errContains    string
	}{
		{
			name:           "dobin -> default",
			section:        "dobin",
			src:            "bin/mycli",
			dst:            "",
			expectedFamily: StateFamilyBin,
			expectedDir:    "/usr",
			expectedBase:   "mycli",
		},
		{
			name:           "newbin rename",
			section:        "dobin",
			src:            "bin/mycli_v2",
			dst:            "mycli",
			expectedFamily: StateFamilyBin,
			expectedDir:    "/usr",
			expectedBase:   "mycli",
		},
		{
			name:           "dobin under custom into root",
			section:        "dobin",
			src:            "bin/mycli",
			dst:            "/usr/local/bin/mycli",
			expectedFamily: StateFamilyBin,
			expectedDir:    "/usr/local",
			expectedBase:   "mycli",
		},
		{
			name:           "dosbin under custom into root",
			section:        "dosbin",
			src:            "sbin/mydaemon",
			dst:            "/opt/foo/sbin/mydaemon",
			expectedFamily: StateFamilyBin,
			expectedDir:    "/opt/foo",
			expectedBase:   "mydaemon",
		},
		{
			name:           "doexe with exeinto",
			section:        "doexe",
			src:            "prog",
			dst:            "/opt/myprog/prog",
			defaultDir:     "/opt/bin",
			expectedFamily: StateFamilyExe,
			expectedDir:    "/opt/myprog",
			expectedBase:   "prog",
		},
		{
			name:           "doexe with defaultDir fallback",
			section:        "doexe",
			src:            "prog",
			dst:            "",
			defaultDir:     "/opt/bin",
			expectedFamily: StateFamilyExe,
			expectedDir:    "/opt/bin",
			expectedBase:   "prog",
		},
		{
			name:           "doins with insinto",
			section:        "doins",
			src:            "config.yaml",
			dst:            "/etc/myapp/config.yaml",
			defaultDir:     "/",
			expectedFamily: StateFamilyIns,
			expectedDir:    "/etc/myapp",
			expectedBase:   "config.yaml",
		},
		{
			name:           "systemd_newunit two arguments",
			section:        "systemd",
			src:            "app.service",
			dst:            "/usr/lib/systemd/system/app-custom.service",
			expectedFamily: StateFamilyNone,
			expectedDir:    "/usr/lib/systemd/system",
			expectedBase:   "app-custom.service",
		},
		{
			name:           "fixed-destination helper valid rename (doconfd)",
			section:        "doconfd",
			src:            "foo.confd",
			dst:            "/etc/conf.d/foo",
			expectedFamily: StateFamilyNone,
			expectedDir:    "/etc/conf.d",
			expectedBase:   "foo",
		},
		{
			name:           "fixed-destination helper valid rename (doinitd)",
			section:        "doinitd",
			src:            "foo.init",
			dst:            "foo",
			expectedFamily: StateFamilyNone,
			expectedDir:    "",
			expectedBase:   "foo",
		},
		{
			name:           "fixed-destination helper valid rename (doheader)",
			section:        "doheader",
			src:            "foo_impl.h",
			dst:            "/usr/include/foo.h",
			expectedFamily: StateFamilyNone,
			expectedDir:    "/usr/include",
			expectedBase:   "foo.h",
		},
		{
			name:        "fixed-destination helper incompatible directory (doconfd)",
			section:     "doconfd",
			src:         "foo.confd",
			dst:         "/etc/foo/foo.confd",
			expectErr:   true,
			errContains: "incompatible with doconfd",
		},
		{
			name:        "fixed-destination helper incompatible directory (doenvd)",
			section:     "doenvd",
			src:         "99foo",
			dst:         "/etc/custom/99foo",
			expectErr:   true,
			errContains: "incompatible with doenvd",
		},
		{
			name:        "fixed-destination helper incompatible directory (doinitd)",
			section:     "doinitd",
			src:         "foo.init",
			dst:         "/etc/rc.d/foo",
			expectErr:   true,
			errContains: "incompatible with doinitd",
		},
		{
			name:        "fixed-destination helper incompatible directory (doheader)",
			section:     "doheader",
			src:         "foo.h",
			dst:         "/usr/local/include/foo.h",
			expectErr:   true,
			errContains: "incompatible with doheader",
		},
		{
			name:        "fixed-destination helper incompatible directory (systemd)",
			section:     "systemd",
			src:         "foo.service",
			dst:         "/etc/systemd/system/foo.service",
			expectErr:   true,
			errContains: "incompatible with systemd",
		},
		{
			name:        "dobin incompatible directory",
			section:     "dobin",
			src:         "bin/cli",
			dst:         "/usr/local/custom/cli",
			expectErr:   true,
			errContains: "incompatible with dobin",
		},
		{
			name:        "dosbin incompatible directory",
			section:     "dosbin",
			src:         "sbin/daemon",
			dst:         "/opt/bin/daemon",
			expectErr:   true,
			errContains: "incompatible with dosbin",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			family, dir, base, err := decomposeDestination(tc.section, tc.src, tc.dst, tc.defaultDir)
			if tc.expectErr {
				require.Error(t, err)
				if tc.errContains != "" {
					require.Contains(t, err.Error(), tc.errContains)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedFamily, family)
				require.Equal(t, tc.expectedDir, dir)
				require.Equal(t, tc.expectedBase, base)
			}
		})
	}
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
