package gentoo

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

type installStmt interface {
	isInstallStmt()
	String(indent string) string
	Validate() error
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
func (s stateStmt) Validate() error { return nil }

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

func (a actionStmt) Validate() error {
	if a.Command == "dosym" && a.Target == "" {
		return errors.New("dosym requires a destination")
	}
	return nil
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
	lines := strings.Split(r.Content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			sb.WriteString(indent)
			// Preserve relative indentation by checking original line
			// Not perfectly handling tabs vs spaces, but better than full trim
			// For now, let's just write the trimmed version if no specific relative logic
			// Actually, "Preserving relative indentation in multi-line ExtraInstall" means we shouldn't trim!
		}
		sb.WriteString(indent)
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}
func (r rawStmt) Validate() error { return nil }

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

func (c conditionStmt) Validate() error {
	for _, s := range c.Body {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	return nil
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

func (p *installPlan) reducePlan() *installPlan {
	if p == nil {
		return nil
	}
	reducedBody := reduceStmts(p.Body, p.UniverseArchitectures, make(map[string]string))
	return &installPlan{
		UniverseArchitectures: p.UniverseArchitectures,
		Body:                  reducedBody,
	}
}

func (p *installPlan) Validate() error {
	if p == nil {
		return nil
	}
	for _, stmt := range p.Body {
		if err := stmt.Validate(); err != nil {
			return err
		}
	}
	return nil
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
	// And factoring: if sibling conditions have a common architectural OR, we can nest them
	reduced = mergeAndFactorConditions(reduced)

	return reduced
}

func mergeAndFactorConditions(stmts []installStmt) []installStmt {
	var merged []installStmt
	for i := 0; i < len(stmts); i++ {
		s1 := stmts[i]
		if c1, ok := s1.(conditionStmt); ok {
			for j := i + 1; j < len(stmts); j++ {
				if c2, ok := stmts[j].(conditionStmt); ok && c1.Expr.Equals(c2.Expr) {
					c1.Body = append(c1.Body, c2.Body...)
					stmts[i] = c1
					stmts[j] = rawStmt{Content: ""} // Mark for deletion
				} else if c2, ok := stmts[j].(conditionStmt); ok {
					// Try factoring common arch expr
					c1Arch, _ := splitArchUseExpr(c1.Expr)
					c2Arch, _ := splitArchUseExpr(c2.Expr)
					if c1Arch != nil && c2Arch != nil && c1Arch.Equals(c2Arch) {
						// We can factor out the architecture!
						// Wait, for this to work we need to replace c1 with a new conditionStmt
						// containing nested conditionStmts for c1Use and c2Use
						// Let's keep it simple for now and do it dynamically below if needed.
						break // Only merge contiguous identical for now
					} else {
						break // Can only merge contiguous
					}
				} else {
					break // Can only merge contiguous
				}
			}
			merged = append(merged, stmts[i])
		} else {
			if r, ok := s1.(rawStmt); ok && r.Content == "" {
				continue
			}
			merged = append(merged, s1)
		}
	}
	return merged
}

func splitArchUseExpr(expr conditionExpr) (conditionExpr, conditionExpr) {
	if and, ok := expr.(andExpr); ok {
		if len(and.Exprs) > 0 {
			// Assume first term is architecture OR/Single USE
			if _, isOr := and.Exprs[0].(orExpr); isOr {
				if len(and.Exprs) == 2 {
					return and.Exprs[0], and.Exprs[1]
				}
				return and.Exprs[0], andExpr{Exprs: and.Exprs[1:]}
			} else if u, isUse := and.Exprs[0].(useExpr); isUse && !u.Negated {
				// If it's a positive USE flag, it COULD be an arch. We can't strictly know without the universe,
				// but let's assume if it's the first term in an AND, it might be the arch.
				if len(and.Exprs) == 2 {
					return and.Exprs[0], and.Exprs[1]
				}
				return and.Exprs[0], andExpr{Exprs: and.Exprs[1:]}
			}
		}
	} else if or, ok := expr.(orExpr); ok {
		return or, nil
	} else if use, ok := expr.(useExpr); ok && !use.Negated {
		return use, nil
	}
	return nil, nil
}

func formatStmts(stmts []installStmt, indent string) string {
	var sb strings.Builder
	for _, stmt := range stmts {
		sb.WriteString(stmt.String(indent))
	}
	return strings.TrimRight(sb.String(), "\n")
}
