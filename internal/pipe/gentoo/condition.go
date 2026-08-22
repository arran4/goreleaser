package gentoo

import (
	"fmt"
	"slices"
	"strings"
)

// conditionExpr represents a boolean condition AST node.
type conditionExpr interface {
	isConditionExpr()
	String() string
	Shell() string
	Equals(other conditionExpr) bool
}

// TrueExpr represents a boolean constant true condition AST node.
type TrueExpr struct{}

func (TrueExpr) isConditionExpr() {}

func (TrueExpr) String() string {
	return "True"
}

func (TrueExpr) Shell() string {
	return "true"
}

func (TrueExpr) Equals(other conditionExpr) bool {
	_, ok := other.(TrueExpr)
	return ok
}

// FalseExpr represents a boolean constant false condition AST node.
type FalseExpr struct{}

func (FalseExpr) isConditionExpr() {}

func (FalseExpr) String() string {
	return "False"
}

func (FalseExpr) Shell() string {
	return "false"
}

func (FalseExpr) Equals(other conditionExpr) bool {
	_, ok := other.(FalseExpr)
	return ok
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
	case TrueExpr:
		return "false"
	case FalseExpr:
		return "true"
	case ArchExpr:
		return "! use " + inner.Arch
	case UseExpr:
		return "! use " + inner.Flag
	case NotExpr:
		return inner.Expr.Shell()
	default:
		return "! { " + n.Expr.Shell() + "; }"
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
		if _, isOr := expr.(OrExpr); isOr {
			parts = append(parts, "{ "+expr.Shell()+"; }")
		} else {
			parts = append(parts, expr.Shell())
		}
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
		if _, isAnd := expr.(AndExpr); isAnd {
			parts = append(parts, "{ "+expr.Shell()+"; }")
		} else {
			parts = append(parts, expr.Shell())
		}
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
	switch e := expr.(type) {
	case TrueExpr:
		return FalseExpr{}
	case FalseExpr:
		return TrueExpr{}
	case NotExpr:
		return e.Expr // NOT(NOT(x)) -> x
	default:
		return NotExpr{Expr: expr}
	}
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

	hasTrue := false
	var filtered []conditionExpr
	for _, e := range flattened {
		if _, isFalse := e.(FalseExpr); isFalse {
			return FalseExpr{}
		}
		if _, isTrue := e.(TrueExpr); isTrue {
			hasTrue = true
			continue
		}
		filtered = append(filtered, e)
	}

	if len(filtered) == 0 {
		if hasTrue {
			return TrueExpr{}
		}
		return nil
	}

	// Deduplicate using Equals
	var unique []conditionExpr
	for _, e := range filtered {
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

	hasFalse := false
	var filtered []conditionExpr
	for _, e := range flattened {
		if _, isTrue := e.(TrueExpr); isTrue {
			return TrueExpr{}
		}
		if _, isFalse := e.(FalseExpr); isFalse {
			hasFalse = true
			continue
		}
		filtered = append(filtered, e)
	}

	if len(filtered) == 0 {
		if hasFalse {
			return FalseExpr{}
		}
		return nil
	}

	// Deduplicate using Equals
	var unique []conditionExpr
	for _, e := range filtered {
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

// simplifyExpr recursively simplifies universal architecture predicates anywhere in an expression tree.
func simplifyExpr(expr conditionExpr, universe []string) conditionExpr {
	if expr == nil {
		return nil
	}
	if len(universe) == 0 {
		return expr
	}
	if isUniversalArchExpr(expr, universe) {
		return TrueExpr{}
	}
	switch e := expr.(type) {
	case TrueExpr, FalseExpr:
		return e
	case ArchExpr:
		if len(universe) == 1 && universe[0] == e.Arch {
			return TrueExpr{}
		}
		return e
	case NotExpr:
		simplified := simplifyExpr(e.Expr, universe)
		if simplified == nil {
			return nil
		}
		return NewNotExpr(simplified)
	case AndExpr:
		var terms []conditionExpr
		for _, sub := range e.Exprs {
			s := simplifyExpr(sub, universe)
			if s != nil {
				terms = append(terms, s)
			}
		}
		return NewAndExpr(terms...)
	case OrExpr:
		// Check if the arch terms in OrExpr cover the universe
		var archs []string
		for _, sub := range e.Exprs {
			if arch, ok := sub.(ArchExpr); ok {
				archs = append(archs, arch.Arch)
			}
		}
		if len(archs) > 0 && len(universe) > 0 {
			coversAll := true
			for _, u := range universe {
				if !slices.Contains(archs, u) {
					coversAll = false
					break
				}
			}
			if coversAll {
				return TrueExpr{}
			}
		}

		var terms []conditionExpr
		for _, sub := range e.Exprs {
			if isUniversalArchExpr(sub, universe) {
				return TrueExpr{}
			}
			s := simplifyExpr(sub, universe)
			if s != nil {
				terms = append(terms, s)
			}
		}
		return NewOrExpr(terms...)
	default:
		return expr
	}
}
