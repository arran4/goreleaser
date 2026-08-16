package gentoo

import (
	"maps"
	"slices"
	"strings"
)

type installStmt interface {
	isInstallStmt()
	String(indent string) string
}

type conditionStmt struct {
	Expr conditionExpr
	Body []installStmt
}

func (c conditionStmt) isInstallStmt() {}
func (c conditionStmt) String(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString("if ")
	sb.WriteString(c.Expr.String())
	sb.WriteString("; then\n")
	for _, stmt := range c.Body {
		sb.WriteString(stmt.String(indent + "  "))
	}
	sb.WriteString(indent)
	sb.WriteString("fi\n")
	return sb.String()
}

type stateStmt struct {
	Command string
	Value   string
}

func (s stateStmt) isInstallStmt() {}
func (s stateStmt) String(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString(s.Command)
	if s.Value != "" && s.Value != "/" {
		sb.WriteString(" ")
		sb.WriteString(s.Value)
	} else if s.Value == "/" {
		sb.WriteString(" /")
	}
	sb.WriteString("\n")
	return sb.String()
}

type actionStmt struct {
	Command string
	Source  string
	Target  string
	Die     string
}

func (a actionStmt) isInstallStmt() {}
func (a actionStmt) String(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString(a.Command)
	sb.WriteString(" \"")
	sb.WriteString(a.Source)
	sb.WriteString("\"")
	if a.Target != "" && a.Target != a.Source && strings.HasPrefix(a.Command, "new") {
		sb.WriteString(" \"")
		sb.WriteString(a.Target)
		sb.WriteString("\"")
	} else if a.Command == "dosym" {
		sb.WriteString(" \"")
		sb.WriteString(a.Target)
		sb.WriteString("\"")
	}
	if a.Die != "" {
		sb.WriteString(" || die \"")
		sb.WriteString(a.Die)
		sb.WriteString("\"")
	}
	sb.WriteString("\n")
	return sb.String()
}

type rawStmt struct {
	Content string
}

func (r rawStmt) isInstallStmt() {}
func (r rawStmt) String(indent string) string {
	if r.Content == "" {
		return ""
	}
	var sb strings.Builder
	lines := strings.Split(strings.TrimSpace(r.Content), "\n")
	for _, line := range lines {
		sb.WriteString(indent)
		sb.WriteString(strings.TrimSpace(line))
		sb.WriteString("\n")
	}
	return sb.String()
}

// Expressions for conditions
type conditionExpr interface {
	isConditionExpr()
	String() string
	Equals(other conditionExpr) bool
}

type useExpr struct {
	Flag    string
	Negated bool
}

func (u useExpr) isConditionExpr() {}
func (u useExpr) String() string {
	if u.Negated {
		return "! use " + u.Flag
	}
	return "use " + u.Flag
}
func (u useExpr) Equals(other conditionExpr) bool {
	o, ok := other.(useExpr)
	if !ok {
		return false
	}
	return u.Flag == o.Flag && u.Negated == o.Negated
}

type andExpr struct {
	Exprs []conditionExpr
}

func (a andExpr) isConditionExpr() {}
func (a andExpr) String() string {
	var parts []string
	for _, expr := range a.Exprs {
		parts = append(parts, expr.String())
	}
	return strings.Join(parts, " && ")
}
func (a andExpr) Equals(other conditionExpr) bool {
	o, ok := other.(andExpr)
	if !ok {
		return false
	}
	if len(a.Exprs) != len(o.Exprs) {
		return false
	}
	for i, e := range a.Exprs {
		if !e.Equals(o.Exprs[i]) {
			return false
		}
	}
	return true
}

type orExpr struct {
	Exprs []conditionExpr
}

func (o orExpr) isConditionExpr() {}
func (o orExpr) String() string {
	var parts []string
	for _, expr := range o.Exprs {
		parts = append(parts, expr.String())
	}
	return strings.Join(parts, " || ")
}
func (o orExpr) Equals(other conditionExpr) bool {
	otherOr, ok := other.(orExpr)
	if !ok {
		return false
	}
	if len(o.Exprs) != len(otherOr.Exprs) {
		return false
	}
	for i, e := range o.Exprs {
		if !e.Equals(otherOr.Exprs[i]) {
			return false
		}
	}
	return true
}

func newArchsAndUseExpr(archs []string, uses []string) conditionExpr {
	var terms []conditionExpr

	if len(archs) > 0 {
		var archTerms []conditionExpr
		for _, arch := range archs {
			archTerms = append(archTerms, useExpr{Flag: arch})
		}
		if len(archTerms) == 1 {
			terms = append(terms, archTerms[0])
		} else {
			terms = append(terms, orExpr{Exprs: archTerms})
		}
	}

	for _, use := range uses {
		if rest, ok := strings.CutPrefix(use, "!"); ok {
			terms = append(terms, useExpr{Flag: rest, Negated: true})
		} else {
			terms = append(terms, useExpr{Flag: use})
		}
	}

	if len(terms) == 0 {
		return nil
	}
	if len(terms) == 1 {
		return terms[0]
	}
	return andExpr{Exprs: terms}
}

func isUniversalArchExpr(expr conditionExpr, universe []string) bool {
	if o, ok := expr.(orExpr); ok {
		var archs []string
		for _, e := range o.Exprs {
			if u, ok := e.(useExpr); ok && !u.Negated {
				archs = append(archs, u.Flag)
			} else {
				return false // Has non-arch or negated term in OR
			}
		}
		for _, u := range universe {
			if !slices.Contains(archs, u) {
				return false
			}
		}
		return true
	} else if u, ok := expr.(useExpr); ok && !u.Negated {
		// Single arch
		return len(universe) == 1 && universe[0] == u.Flag
	}
	return false
}

type installPlan struct {
	UniverseArchitectures []string
	Body                  []installStmt
}

func reducePlan(plan *installPlan) *installPlan {
	if plan == nil {
		return nil
	}
	reducedBody := reduceStmts(plan.Body, plan.UniverseArchitectures, make(map[string]string))
	return &installPlan{
		UniverseArchitectures: plan.UniverseArchitectures,
		Body:                  reducedBody,
	}
}

func reduceStmts(stmts []installStmt, universe []string, state map[string]string) []installStmt {
	var reduced []installStmt

	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case conditionStmt:
			// Rule 1: Universal architecture conditions
			if isUniversalArchExpr(s.Expr, universe) {
				sBody := reduceStmts(s.Body, universe, state)
				reduced = append(reduced, sBody...)
				continue
			}

			// Rule 3: Propagate state
			stateBefore := make(map[string]string)
			maps.Copy(stateBefore, state)

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
				if c2, ok := reduced[j].(conditionStmt); ok && c1.Expr.Equals(c2.Expr) {
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
	var sb strings.Builder
	for _, stmt := range stmts {
		sb.WriteString(stmt.String(indent))
	}
	return strings.TrimRight(sb.String(), "\n")
}
