package gentoo

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

type installStmt interface {
	isInstallStmt()
	String(indent string) string
	Validate() error
	Equals(other installStmt) bool
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

func (s stateStmt) Equals(other installStmt) bool {
	o, ok := other.(stateStmt)
	return ok && s.Command == o.Command && s.Value == o.Value
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

func (a actionStmt) Validate() error {
	if a.Command == "dosym" && a.Target == "" {
		return errors.New("dosym requires a destination")
	}
	return nil
}

func (a actionStmt) Equals(other installStmt) bool {
	o, ok := other.(actionStmt)
	return ok && a.Command == o.Command && a.Source == o.Source && a.Target == o.Target && a.Die == o.Die
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
	for line := range strings.SplitSeq(r.Content, "\n") {
		if line != "" {
			sb.WriteString(indent)
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (r rawStmt) Validate() error { return nil }

func (r rawStmt) Equals(other installStmt) bool {
	o, ok := other.(rawStmt)
	return ok && r.Content == o.Content
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
	if c.Expr != nil {
		sb.WriteString(c.Expr.Shell())
	}
	sb.WriteString("; then\n")
	for _, stmt := range c.Body {
		sb.WriteString(stmt.String(indent + "  "))
	}
	sb.WriteString(indent)
	sb.WriteString("fi\n")
	return sb.String()
}

func (c conditionStmt) Validate() error {
	if c.Expr == nil {
		return errors.New("condition requires an expression")
	}
	for _, s := range c.Body {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c conditionStmt) Equals(other installStmt) bool {
	o, ok := other.(conditionStmt)
	if !ok {
		return false
	}
	if (c.Expr == nil) != (o.Expr == nil) {
		return false
	}
	if c.Expr != nil && !c.Expr.Equals(o.Expr) {
		return false
	}
	if len(c.Body) != len(o.Body) {
		return false
	}
	for i := range c.Body {
		if !c.Body[i].Equals(o.Body[i]) {
			return false
		}
	}
	return true
}

// conditionExpr represents a boolean condition AST node.
type conditionExpr interface {
	isConditionExpr()
	String() string
	Shell() string
	Equals(other conditionExpr) bool
}

// ArchExpr represents an architecture condition predicate, e.g. Arch(amd64).
type ArchExpr struct {
	Arch string
}

func (a ArchExpr) isConditionExpr() {}

func (a ArchExpr) String() string {
	return fmt.Sprintf("Arch(%s)", a.Arch)
}

func (a ArchExpr) Shell() string {
	return "use " + a.Arch
}

func (a ArchExpr) Equals(other conditionExpr) bool {
	o, ok := other.(ArchExpr)
	return ok && a.Arch == o.Arch
}

// UseExpr represents a Gentoo USE flag condition predicate, e.g. Use(extended).
type UseExpr struct {
	Flag string
}

func (u UseExpr) isConditionExpr() {}

func (u UseExpr) String() string {
	return fmt.Sprintf("Use(%s)", u.Flag)
}

func (u UseExpr) Shell() string {
	return "use " + u.Flag
}

func (u UseExpr) Equals(other conditionExpr) bool {
	o, ok := other.(UseExpr)
	return ok && u.Flag == o.Flag
}

// NotExpr represents a boolean NOT operation, e.g. NOT(Use(extended)).
type NotExpr struct {
	Expr conditionExpr
}

func (n NotExpr) isConditionExpr() {}

func (n NotExpr) String() string {
	if n.Expr == nil {
		return "NOT()"
	}
	return fmt.Sprintf("NOT(%s)", n.Expr.String())
}

func (n NotExpr) Shell() string {
	if n.Expr == nil {
		return ""
	}
	switch inner := n.Expr.(type) {
	case ArchExpr:
		return "! use " + inner.Arch
	case UseExpr:
		return "! use " + inner.Flag
	case NotExpr:
		return inner.Expr.Shell()
	default:
		return "! " + n.Expr.Shell()
	}
}

func (n NotExpr) Equals(other conditionExpr) bool {
	o, ok := other.(NotExpr)
	if !ok {
		return false
	}
	if (n.Expr == nil) != (o.Expr == nil) {
		return false
	}
	if n.Expr != nil && !n.Expr.Equals(o.Expr) {
		return false
	}
	return true
}

// AndExpr represents a boolean AND operation, e.g. AND(Arch(amd64), Use(extended)).
type AndExpr struct {
	Exprs []conditionExpr
}

func (a AndExpr) isConditionExpr() {}

func (a AndExpr) String() string {
	var parts []string
	for _, expr := range a.Exprs {
		parts = append(parts, expr.String())
	}
	return fmt.Sprintf("AND(%s)", strings.Join(parts, ", "))
}

func (a AndExpr) Shell() string {
	var parts []string
	for _, expr := range a.Exprs {
		parts = append(parts, expr.Shell())
	}
	return strings.Join(parts, " && ")
}

func (a AndExpr) Equals(other conditionExpr) bool {
	o, ok := other.(AndExpr)
	if !ok || len(a.Exprs) != len(o.Exprs) {
		return false
	}
	for i, e := range a.Exprs {
		if !e.Equals(o.Exprs[i]) {
			return false
		}
	}
	return true
}

// OrExpr represents a boolean OR operation, e.g. OR(Arch(amd64), Arch(arm64)).
type OrExpr struct {
	Exprs []conditionExpr
}

func (o OrExpr) isConditionExpr() {}

func (o OrExpr) String() string {
	var parts []string
	for _, expr := range o.Exprs {
		parts = append(parts, expr.String())
	}
	return fmt.Sprintf("OR(%s)", strings.Join(parts, ", "))
}

func (o OrExpr) Shell() string {
	var parts []string
	for _, expr := range o.Exprs {
		parts = append(parts, expr.Shell())
	}
	return strings.Join(parts, " || ")
}

func (o OrExpr) Equals(other conditionExpr) bool {
	otherOr, ok := other.(OrExpr)
	if !ok || len(o.Exprs) != len(otherOr.Exprs) {
		return false
	}
	for i, e := range o.Exprs {
		if !e.Equals(otherOr.Exprs[i]) {
			return false
		}
	}
	return true
}

// NewNotExpr creates a normalized NOT expression.
func NewNotExpr(expr conditionExpr) conditionExpr {
	if expr == nil {
		return nil
	}
	if n, ok := expr.(NotExpr); ok {
		return n.Expr // NOT(NOT(x)) -> x
	}
	return NotExpr{Expr: expr}
}

// NewAndExpr creates a canonicalized, flattened, deduplicated AND expression.
func NewAndExpr(exprs ...conditionExpr) conditionExpr {
	var flattened []conditionExpr
	for _, e := range exprs {
		if e == nil {
			continue
		}
		if a, ok := e.(AndExpr); ok {
			for _, sub := range a.Exprs {
				if sub != nil {
					flattened = append(flattened, sub)
				}
			}
		} else {
			flattened = append(flattened, e)
		}
	}

	if len(flattened) == 0 {
		return nil
	}

	// Deduplicate using Equals
	var unique []conditionExpr
	for _, e := range flattened {
		if !slices.ContainsFunc(unique, func(u conditionExpr) bool {
			return e.Equals(u)
		}) {
			unique = append(unique, e)
		}
	}

	// Canonical sort
	slices.SortFunc(unique, func(a, b conditionExpr) int {
		return strings.Compare(a.String(), b.String())
	})

	if len(unique) == 1 {
		return unique[0]
	}
	return AndExpr{Exprs: unique}
}

// NewOrExpr creates a canonicalized, flattened, deduplicated OR expression.
func NewOrExpr(exprs ...conditionExpr) conditionExpr {
	var flattened []conditionExpr
	for _, e := range exprs {
		if e == nil {
			continue
		}
		if o, ok := e.(OrExpr); ok {
			for _, sub := range o.Exprs {
				if sub != nil {
					flattened = append(flattened, sub)
				}
			}
		} else {
			flattened = append(flattened, e)
		}
	}

	if len(flattened) == 0 {
		return nil
	}

	// Deduplicate using Equals
	var unique []conditionExpr
	for _, e := range flattened {
		if !slices.ContainsFunc(unique, func(u conditionExpr) bool {
			return e.Equals(u)
		}) {
			unique = append(unique, e)
		}
	}

	// Canonical sort
	slices.SortFunc(unique, func(a, b conditionExpr) int {
		return strings.Compare(a.String(), b.String())
	})

	if len(unique) == 1 {
		return unique[0]
	}
	return OrExpr{Exprs: unique}
}

func newArchsAndUseExpr(archs []string, uses []string) conditionExpr {
	var terms []conditionExpr

	if len(archs) > 0 {
		var archTerms []conditionExpr
		for _, arch := range archs {
			archTerms = append(archTerms, ArchExpr{Arch: arch})
		}
		if len(archTerms) == 1 {
			terms = append(terms, archTerms[0])
		} else {
			terms = append(terms, NewOrExpr(archTerms...))
		}
	}

	for _, use := range uses {
		if rest, ok := strings.CutPrefix(use, "!"); ok {
			terms = append(terms, NewNotExpr(UseExpr{Flag: rest}))
		} else {
			terms = append(terms, UseExpr{Flag: use})
		}
	}

	if len(terms) == 0 {
		return nil
	}
	if len(terms) == 1 {
		return terms[0]
	}
	return NewAndExpr(terms...)
}

func isUniversalArchExpr(expr conditionExpr, universe []string) bool {
	if len(universe) == 0 || expr == nil {
		return false
	}
	switch e := expr.(type) {
	case ArchExpr:
		return len(universe) == 1 && universe[0] == e.Arch
	case OrExpr:
		var archs []string
		for _, term := range e.Exprs {
			if arch, ok := term.(ArchExpr); ok {
				archs = append(archs, arch.Arch)
			} else {
				return false
			}
		}
		for _, u := range universe {
			if !slices.Contains(archs, u) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

type installPlan struct {
	UniverseArchitectures []string
	Body                  []installStmt
}

func (p *installPlan) reducePlan() *installPlan {
	if p == nil {
		return nil
	}
	current := p
	for range 10 {
		next := &installPlan{
			UniverseArchitectures: current.UniverseArchitectures,
			Body:                  reduceStmts(current.Body, current.UniverseArchitectures, make(map[string]string)),
		}
		if planEqual(current, next) {
			return next
		}
		current = next
	}
	return current
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

func planEqual(a, b *installPlan) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !slices.Equal(a.UniverseArchitectures, b.UniverseArchitectures) {
		return false
	}
	if len(a.Body) != len(b.Body) {
		return false
	}
	for i := range a.Body {
		if !a.Body[i].Equals(b.Body[i]) {
			return false
		}
	}
	return true
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

			// Rule 3: Propagate state into branch
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

			// Join state: any key mutated in branch is now uncertain (divergent)
			for k := range state {
				if branchState[k] != stateBefore[k] {
					delete(state, k)
				}
			}
			for k, v := range branchState {
				if _, exists := stateBefore[k]; !exists {
					delete(state, k)
				} else if stateBefore[k] != v {
					delete(state, k)
				}
			}

		case stateStmt:
			// Rule 2: Redundant state setters
			if val, ok := state[s.Command]; ok && val == s.Value {
				continue // redundant
			}
			state[s.Command] = s.Value
			reduced = append(reduced, s)

		default:
			reduced = append(reduced, s)
		}
	}

	// Merge and factor sibling conditions
	reduced = mergeAndFactorSiblingConditions(reduced)

	return reduced
}

func mergeAndFactorSiblingConditions(stmts []installStmt) []installStmt {
	var result []installStmt
	i := 0
	for i < len(stmts) {
		_, isCond := stmts[i].(conditionStmt)
		if !isCond {
			if raw, isRaw := stmts[i].(rawStmt); isRaw && raw.Content == "" {
				i++
				continue
			}
			result = append(result, stmts[i])
			i++
			continue
		}

		// Collect contiguous conditionStmts
		var conds []conditionStmt
		j := i
		for j < len(stmts) {
			c, ok := stmts[j].(conditionStmt)
			if !ok {
				break
			}
			conds = append(conds, c)
			j++
		}

		factored := factorConditionsSlice(conds)
		result = append(result, factored...)
		i = j
	}
	return result
}

func factorConditionsSlice(conds []conditionStmt) []installStmt {
	if len(conds) == 0 {
		return nil
	}
	if len(conds) == 1 {
		return []installStmt{conds[0]}
	}

	var result []installStmt
	i := 0
	for i < len(conds) {
		bestJ := i
		var bestFactor conditionExpr

		for j := len(conds) - 1; j > i; j-- {
			factors := findCommonFactors(conds[i : j+1])
			if len(factors) > 0 {
				bestJ = j
				bestFactor = selectBestFactor(factors)
				break
			}
		}

		if bestJ > i && bestFactor != nil {
			var innerBody []installStmt
			for k := i; k <= bestJ; k++ {
				rem := removeTerm(conds[k].Expr, bestFactor)
				if rem == nil {
					innerBody = append(innerBody, conds[k].Body...)
				} else {
					innerBody = append(innerBody, conditionStmt{
						Expr: rem,
						Body: conds[k].Body,
					})
				}
			}
			factoredCond := conditionStmt{
				Expr: bestFactor,
				Body: mergeAndFactorSiblingConditions(innerBody),
			}
			result = append(result, factoredCond)
			i = bestJ + 1
		} else {
			result = append(result, conds[i])
			i++
		}
	}
	return result
}

func getTerms(expr conditionExpr) []conditionExpr {
	if expr == nil {
		return nil
	}
	if and, ok := expr.(AndExpr); ok {
		return and.Exprs
	}
	return []conditionExpr{expr}
}

func findCommonFactors(slice []conditionStmt) []conditionExpr {
	if len(slice) == 0 {
		return nil
	}
	common := getTerms(slice[0].Expr)
	for _, c := range slice[1:] {
		cTerms := getTerms(c.Expr)
		var nextCommon []conditionExpr
		for _, f := range common {
			if slices.ContainsFunc(cTerms, func(ct conditionExpr) bool {
				return f.Equals(ct)
			}) {
				nextCommon = append(nextCommon, f)
			}
		}
		common = nextCommon
		if len(common) == 0 {
			break
		}
	}
	return common
}

func selectBestFactor(factors []conditionExpr) conditionExpr {
	if len(factors) == 0 {
		return nil
	}
	slices.SortFunc(factors, func(a, b conditionExpr) int {
		pA := factorPriority(a)
		pB := factorPriority(b)
		if pA != pB {
			return pA - pB
		}
		return strings.Compare(a.String(), b.String())
	})
	return factors[0]
}

func factorPriority(e conditionExpr) int {
	switch e.(type) {
	case ArchExpr, OrExpr:
		return 1
	case UseExpr:
		return 2
	case NotExpr:
		return 3
	default:
		return 4
	}
}

func removeTerm(expr conditionExpr, term conditionExpr) conditionExpr {
	if expr == nil || term == nil {
		return expr
	}
	if expr.Equals(term) {
		return nil
	}
	if and, ok := expr.(AndExpr); ok {
		var remaining []conditionExpr
		for _, t := range and.Exprs {
			if !t.Equals(term) {
				remaining = append(remaining, t)
			}
		}
		if len(remaining) == 0 {
			return nil
		}
		if len(remaining) == 1 {
			return remaining[0]
		}
		return NewAndExpr(remaining...)
	}
	return expr
}

func formatStmts(stmts []installStmt, indent string) string {
	var sb strings.Builder
	for _, stmt := range stmts {
		sb.WriteString(stmt.String(indent))
	}
	return strings.TrimRight(sb.String(), "\n")
}
