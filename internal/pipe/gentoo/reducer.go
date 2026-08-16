package gentoo

import (
	"maps"
	"slices"
	"strings"
)

type installStmt interface {
	isInstallStmt()
}

type conditionStmt struct {
	Architectures []string
	Use           []string
	Body          []installStmt
}

func (c conditionStmt) isInstallStmt() {}

type stateStmt struct {
	Command string
	Value   string
}

func (s stateStmt) isInstallStmt() {}

type actionStmt struct {
	Command string
	Source  string
	Target  string
	Die     string
}

func (a actionStmt) isInstallStmt() {}

type rawStmt struct {
	Content string
}

func (r rawStmt) isInstallStmt() {}

type installPlan struct {
	UniverseArchitectures []string
	Body                  []installStmt
}

func reducePlan(plan installPlan) installPlan {
	return installPlan{
		UniverseArchitectures: plan.UniverseArchitectures,
		Body:                  reduceStmts(plan.Body, plan.UniverseArchitectures, make(map[string]string)),
	}
}

func reduceStmts(stmts []installStmt, universe []string, state map[string]string) []installStmt {
	var reduced []installStmt

	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case conditionStmt:
			// Rule 1: Universal architecture conditions
			if len(s.Architectures) > 0 && len(s.Use) == 0 {
				isUniversal := true
				for _, u := range universe {
					if !slices.Contains(s.Architectures, u) {
						isUniversal = false
						break
					}
				}
				if isUniversal {
					// Inline body
					sBody := reduceStmts(s.Body, universe, state)
					reduced = append(reduced, sBody...)
					continue
				}
			}

			// Rule 3: Propagate state
			stateBefore := make(map[string]string)
			maps.Copy(stateBefore, state)

			// We need to determine if state is divergent after the conditional
			branchState := make(map[string]string)
			maps.Copy(branchState, stateBefore)

			reducedBody := reduceStmts(s.Body, universe, branchState)
			if len(reducedBody) == 0 {
				continue
			}

			s.Body = reducedBody
			reduced = append(reduced, s)

			// Join state
			for k := range state {
				if branchState[k] != stateBefore[k] {
					delete(state, k) // divergent
				}
			}

		case stateStmt:
			// Rule 2: Redundant state setters
			if state[s.Command] == s.Value {
				continue // redundant
			}
			state[s.Command] = s.Value
			reduced = append(reduced, s)

		default:
			reduced = append(reduced, s)
		}
	}

	// Merge adjacent identical conditions (Rule 5 simple case)
	var merged []installStmt
	for i := 0; i < len(reduced); i++ {
		s1 := reduced[i]
		if c1, ok := s1.(conditionStmt); ok {
			for j := i + 1; j < len(reduced); j++ {
				if c2, ok := reduced[j].(conditionStmt); ok && slices.Equal(c1.Architectures, c2.Architectures) && slices.Equal(c1.Use, c2.Use) {
					c1.Body = append(c1.Body, c2.Body...)
					reduced[i] = c1
					reduced[j] = rawStmt{Content: ""} // Mark for deletion
				} else {
					break // Can only merge contiguous
				}
			}
			merged = append(merged, reduced[i])
		} else {
			if r, ok := s1.(rawStmt); ok && r.Content == "" {
				continue
			}
			merged = append(merged, s1)
		}
	}

	return merged
}

func formatStmts(stmts []installStmt, indent string) string {
	return strings.TrimRight(formatStmtsInternal(stmts, indent), "\n")
}

func formatStmtsInternal(stmts []installStmt, indent string) string {
	var sb strings.Builder
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case conditionStmt:
			conds := []string{}
			if len(s.Architectures) > 0 {
				var use []string
				for _, a := range s.Architectures {
					use = append(use, "use "+a)
				}
				conds = append(conds, strings.Join(use, " || "))
			}
			for _, u := range s.Use {
				if rest, ok := strings.CutPrefix(u, "!"); ok {
					conds = append(conds, "! use "+rest)
				} else {
					conds = append(conds, "use "+u)
				}
			}
			sb.WriteString(indent)
			sb.WriteString("if ")
			sb.WriteString(strings.Join(conds, " && "))
			sb.WriteString("; then\n")
			sb.WriteString(formatStmtsInternal(s.Body, indent+"  "))
			sb.WriteString(indent)
			sb.WriteString("fi\n")
		case stateStmt:
			sb.WriteString(indent)
			sb.WriteString(s.Command)
			if s.Value != "" && s.Value != "/" {
				sb.WriteString(" ")
				sb.WriteString(s.Value)
			} else if s.Value == "/" {
				sb.WriteString(" /")
			}
			sb.WriteString("\n")
		case actionStmt:
			sb.WriteString(indent)
			sb.WriteString(s.Command)
			sb.WriteString(` "`)
			sb.WriteString(s.Source)
			sb.WriteString(`"`)
			if s.Target != "" && s.Target != s.Source {
				sb.WriteString(` "`)
				sb.WriteString(s.Target)
				sb.WriteString(`"`)
			}
			if s.Die != "" {
				sb.WriteString(" || die \"")
				sb.WriteString(s.Die)
				sb.WriteString("\"")
			}
			sb.WriteString("\n")
		case rawStmt:
			// Raw statement handling handles ExtraInstall which could be multiple lines
			lines := strings.Split(strings.TrimSpace(s.Content), "\n")
			for _, line := range lines {
				sb.WriteString(indent)
				sb.WriteString(strings.TrimSpace(line))
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}
