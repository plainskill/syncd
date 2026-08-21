package fanout

import (
	"fmt"
	"strings"
)

type Decision int

const (
	DecSkip Decision = iota
	DecFF
	DecFanout
	DecMerge
)

func Decide(fj, in string, fjAncIn, inAncFj bool) Decision {
	if in == "" {
		return DecSkip
	}
	if fj == "" || fj == in {
		return DecFF
	}
	if fjAncIn {
		return DecFF
	}
	if inAncFj {
		return DecFanout
	}
	return DecMerge
}

func PushOrder(source string, includeSource bool) []string {
	var order []string
	switch source {
	case "github":
		order = []string{"forgejo", "gitlawb"}
	case "forgejo":
		order = []string{"github", "gitlawb"}
	case "gitlawb":
		order = []string{"forgejo", "github"}
	default:
		order = []string{"forgejo", "gitlawb", "github"}
	}
	if includeSource && source != "" {
		order = append(order, source)
	}
	return order
}

func ConflictBranch(source, sha string) string {
	short := sha
	if len(short) > 7 {
		short = short[:7]
	}
	src := source
	if src == "" {
		src = "unknown"
	}
	return "sync/" + src + "/" + short
}

func IsTag(ref string) bool {
	return strings.HasPrefix(ref, "refs/tags/")
}

func ShortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func MergeMsg(source, sha string) string {
	return fmt.Sprintf("sync: merge %s %s", source, ShortSHA(sha))
}
