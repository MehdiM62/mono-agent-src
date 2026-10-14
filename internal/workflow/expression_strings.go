package workflow

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"text/template"
)

// addStringMatchFuncs adds contains, startsWith, endsWith and matches. Like
// replace and split, the string tested comes last so the functions pipe:
// {{ $json.title | contains "go" }}. A nil value tests as "".
func addStringMatchFuncs(m template.FuncMap) {
	m["contains"] = func(sub string, s interface{}) bool { return strings.Contains(exprStr(s), sub) }
	m["startsWith"] = func(prefix string, s interface{}) bool { return strings.HasPrefix(exprStr(s), prefix) }
	m["endsWith"] = func(suffix string, s interface{}) bool { return strings.HasSuffix(exprStr(s), suffix) }
	m["matches"] = func(pattern string, s interface{}) (bool, error) {
		re, err := cachedRegexp(pattern)
		if err != nil {
			return false, err
		}
		return re.MatchString(exprStr(s)), nil
	}
}

func exprStr(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	return fmt.Sprintf("%v", v)
}

// maxCachedRegexps bounds the compiled-pattern cache; patterns past it are
// compiled on every use.
const maxCachedRegexps = 512

var (
	regexpMu    sync.Mutex
	regexpCache = map[string]*regexp.Regexp{}
)

func cachedRegexp(pattern string) (*regexp.Regexp, error) {
	regexpMu.Lock()
	re, ok := regexpCache[pattern]
	regexpMu.Unlock()
	if ok {
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("matches: %w", err)
	}
	regexpMu.Lock()
	if len(regexpCache) < maxCachedRegexps {
		regexpCache[pattern] = re
	}
	regexpMu.Unlock()
	return re, nil
}
