package gentoo

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var gentooPrereleaseRe = regexp.MustCompile(`(?i)-(alpha|beta|pre|rc|p)[.\-]?(\d*)`)
var gentooSuffixTokenRe = regexp.MustCompile(`_(alpha|beta|pre|rc|p)(\d*)$`)

type VersionBucket string

const (
	BucketStable VersionBucket = "stable"
	BucketAlpha  VersionBucket = "alpha"
	BucketBeta   VersionBucket = "beta"
	BucketPre    VersionBucket = "pre"
	BucketRc     VersionBucket = "rc"
)

type suffixKind int

const (
	suffixAlpha suffixKind = 1
	suffixBeta  suffixKind = 2
	suffixPre   suffixKind = 3
	suffixRc    suffixKind = 4
	suffixP     suffixKind = 5
)

type gentooSuffix struct {
	kind suffixKind
	val  int
}

type GentooVersion struct {
	raw        string
	baseNum    []int
	baseNumStr []string
	baseLetter rune
	suffixes   []gentooSuffix
	revision   int
}

func GentooVersionFromRelease(v string, from string) (string, error) {
	switch from {
	case "gentoo-version":
		converted := gentooPrereleaseRe.ReplaceAllStringFunc(v, func(m string) string {
			match := gentooPrereleaseRe.FindStringSubmatch(m)
			return "_" + strings.ToLower(match[1]) + match[2]
		})
		if _, err := ParseGentooVersion(converted + ".ebuild"); err != nil {
			return "", fmt.Errorf("version %q cannot be naturally represented in Gentoo", v)
		}
		return converted, nil
	default:
		return "", fmt.Errorf("unsupported version representation %v", from)
	}
}

func ParseGentooVersion(n string) (GentooVersion, error) {
	vStr := strings.TrimSuffix(n, ".ebuild")
	if vStr == "" || vStr == n {
		return GentooVersion{}, fmt.Errorf("invalid gentoo version: %s", n)
	}

	var rev int
	if idx := strings.LastIndex(vStr, "-r"); idx != -1 {
		if parsedRev, err := strconv.Atoi(vStr[idx+2:]); err == nil {
			rev = parsedRev
			vStr = vStr[:idx]
		}
	}

	var suffixes []gentooSuffix
	for {
		loc := gentooSuffixTokenRe.FindStringSubmatchIndex(vStr)
		if loc == nil {
			break
		}
		kindStr := vStr[loc[2]:loc[3]]
		valStr := vStr[loc[4]:loc[5]]
		val := 0
		if valStr != "" {
			var err error
			val, err = strconv.Atoi(valStr)
			if err != nil {
				return GentooVersion{}, fmt.Errorf("invalid gentoo version suffix value: %s", n)
			}
		}

		var kind suffixKind
		switch kindStr {
		case "alpha":
			kind = suffixAlpha
		case "beta":
			kind = suffixBeta
		case "pre":
			kind = suffixPre
		case "rc":
			kind = suffixRc
		case "p":
			kind = suffixP
		default:
			return GentooVersion{}, fmt.Errorf("invalid gentoo version suffix kind: %s", n)
		}

		suffixes = append([]gentooSuffix{{kind: kind, val: val}}, suffixes...)
		vStr = vStr[:loc[0]]
	}

	if vStr == "" {
		return GentooVersion{}, fmt.Errorf("invalid gentoo version: %s", n)
	}

	var letter rune
	lastChar := vStr[len(vStr)-1]
	if lastChar >= 'a' && lastChar <= 'z' {
		letter = rune(lastChar)
		vStr = vStr[:len(vStr)-1]
	}

	if vStr == "" {
		return GentooVersion{}, fmt.Errorf("invalid gentoo version: %s", n)
	}

	parts := strings.Split(vStr, ".")
	var baseNum []int
	var baseNumStr []string
	for _, p := range parts {
		if p == "" {
			return GentooVersion{}, fmt.Errorf("invalid gentoo version: %s", n)
		}
		num, err := strconv.Atoi(p)
		if err != nil {
			return GentooVersion{}, fmt.Errorf("invalid gentoo version numeric base: %s", n)
		}
		baseNum = append(baseNum, num)
		baseNumStr = append(baseNumStr, p)
	}

	return GentooVersion{
		raw:        n,
		baseNum:    baseNum,
		baseNumStr: baseNumStr,
		baseLetter: letter,
		suffixes:   suffixes,
		revision:   rev,
	}, nil
}

func (v GentooVersion) Compare(other GentooVersion) int {
	if len(v.baseNum) > 0 && len(other.baseNum) > 0 {
		if v.baseNum[0] < other.baseNum[0] {
			return -1
		}
		if v.baseNum[0] > other.baseNum[0] {
			return 1
		}
	}

	minLen := min(len(v.baseNum), len(other.baseNum))
	for i := 1; i < minLen; i++ {
		s1 := v.baseNumStr[i]
		s2 := other.baseNumStr[i]
		if strings.HasPrefix(s1, "0") || strings.HasPrefix(s2, "0") {
			s1Trim := strings.TrimRight(s1, "0")
			s2Trim := strings.TrimRight(s2, "0")
			if s1Trim < s2Trim {
				return -1
			}
			if s1Trim > s2Trim {
				return 1
			}
		} else {
			if v.baseNum[i] < other.baseNum[i] {
				return -1
			}
			if v.baseNum[i] > other.baseNum[i] {
				return 1
			}
		}
	}

	if len(v.baseNum) < len(other.baseNum) {
		return -1
	}
	if len(v.baseNum) > len(other.baseNum) {
		return 1
	}

	if v.baseLetter < other.baseLetter {
		return -1
	}
	if v.baseLetter > other.baseLetter {
		return 1
	}

	cmpSuffix := compareGentooSuffixes(v.suffixes, other.suffixes)
	if cmpSuffix != 0 {
		return cmpSuffix
	}

	if v.revision < other.revision {
		return -1
	}
	if v.revision > other.revision {
		return 1
	}

	return 0
}

func compareGentooSuffixes(s1, s2 []gentooSuffix) int {
	if len(s1) == 0 && len(s2) == 0 {
		return 0
	}
	if len(s1) == 0 {
		if s2[0].kind == suffixP {
			return -1 // release < _p
		}
		return 1 // release > _alpha, _beta, _pre, _rc
	}
	if len(s2) == 0 {
		if s1[0].kind == suffixP {
			return 1 // _p > release
		}
		return -1 // _alpha, _beta, _pre, _rc < release
	}

	maxLen := max(len(s1), len(s2))
	for i := range maxLen {
		if i >= len(s1) {
			if s2[i].kind == suffixP {
				return -1
			}
			return 1
		}
		if i >= len(s2) {
			if s1[i].kind == suffixP {
				return 1
			}
			return -1
		}
		if s1[i].kind < s2[i].kind {
			return -1
		}
		if s1[i].kind > s2[i].kind {
			return 1
		}
		if s1[i].val < s2[i].val {
			return -1
		}
		if s1[i].val > s2[i].val {
			return 1
		}
	}
	return 0
}

func (v GentooVersion) GreaterThan(other GentooVersion) bool {
	return v.Compare(other) > 0
}

func (v GentooVersion) BaseEqual(other GentooVersion) bool {
	if len(v.baseNum) != len(other.baseNum) || v.baseLetter != other.baseLetter || len(v.suffixes) != len(other.suffixes) {
		return false
	}
	for i := range v.baseNum {
		if v.baseNum[i] != other.baseNum[i] || v.baseNumStr[i] != other.baseNumStr[i] {
			return false
		}
	}
	for i := range v.suffixes {
		if v.suffixes[i] != other.suffixes[i] {
			return false
		}
	}
	return true
}

func (v GentooVersion) WithoutRevision() GentooVersion {
	c := v
	c.revision = 0
	return c
}

func (v GentooVersion) WithRevision(n int) GentooVersion {
	c := v
	c.revision = n
	return c
}

func (v GentooVersion) Revision() int {
	return v.revision
}

func (v GentooVersion) Bucket() VersionBucket {
	if len(v.suffixes) == 0 {
		return BucketStable
	}
	last := v.suffixes[len(v.suffixes)-1]
	switch last.kind {
	case suffixAlpha:
		return BucketAlpha
	case suffixBeta:
		return BucketBeta
	case suffixPre:
		return BucketPre
	case suffixRc:
		return BucketRc
	default:
		return BucketStable
	}
}

func (v GentooVersion) String() string {
	var sb strings.Builder
	for i, num := range v.baseNumStr {
		if i > 0 {
			sb.WriteString(".")
		}
		sb.WriteString(num)
	}
	if v.baseLetter != 0 {
		sb.WriteRune(v.baseLetter)
	}
	for _, suf := range v.suffixes {
		sb.WriteString("_")
		switch suf.kind {
		case suffixAlpha:
			sb.WriteString("alpha")
		case suffixBeta:
			sb.WriteString("beta")
		case suffixPre:
			sb.WriteString("pre")
		case suffixRc:
			sb.WriteString("rc")
		case suffixP:
			sb.WriteString("p")
		}
		if suf.val > 0 {
			sb.WriteString(strconv.Itoa(suf.val))
		}
	}
	if v.revision > 0 {
		sb.WriteString(fmt.Sprintf("-r%d", v.revision))
	}
	return sb.String()
}
