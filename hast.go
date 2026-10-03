package blocknote2md

// hast.go holds the shared HTML tree (hast), JavaScript-flavoured string
// helpers and hast-util-minify-whitespace; see doc.go for the pipeline.

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ---- hast ----

type hKind int

const (
	hRoot hKind = iota
	hElement
	hText
)

// hNode is the subset of a hast node this pipeline needs. props holds HTML
// attribute values by their HTML name; classes the parsed class list.
type hNode struct {
	kind     hKind
	tag      string
	props    map[string]string
	classes  []string
	children []*hNode
	value    string
}

func hEl(tag string, children ...*hNode) *hNode {
	return &hNode{kind: hElement, tag: tag, props: map[string]string{}, children: children}
}

func hTextNode(v string) *hNode { return &hNode{kind: hText, value: v} }

func (n *hNode) set(k, v string) *hNode {
	n.props[k] = v
	return n
}

func (n *hNode) has(k string) bool {
	_, ok := n.props[k]
	return ok
}

func (n *hNode) get(k string) string { return n.props[k] }

func (n *hNode) add(c ...*hNode) *hNode {
	n.children = append(n.children, c...)
	return n
}

func (n *hNode) isEl(tags ...string) bool {
	if n == nil || n.kind != hElement {
		return false
	}
	for _, t := range tags {
		if n.tag == t {
			return true
		}
	}
	return false
}

func (n *hNode) lastChild() *hNode {
	if len(n.children) == 0 {
		return nil
	}
	return n.children[len(n.children)-1]
}

func hasChildren(n *hNode) bool { return n.kind == hRoot || n.kind == hElement }

// ---- UTF-16 string helpers ----
//
// markdown-table, the emphasis encoder and mdast-util-to-markdown's
// containerPhrasing all index strings by UTF-16 code unit. Go strings are
// UTF-8, so a lone surrogate (what JS slicing can produce next to an emoji)
// is kept internally in WTF-8 (the 3-byte form of the surrogate) and folded
// into a real character, or U+FFFD if it stays lone, by finalizeString.

// toUnits decodes s (UTF-8 plus WTF-8 surrogates) into UTF-16 code units.
func toUnits(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		if u, w, ok := wtf8Surrogate(s[i:]); ok {
			out = append(out, u)
			i += w
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && w == 1 {
			out = append(out, 0xFFFD)
			i++
			continue
		}
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
		i += w
	}
	return out
}

// wtf8Surrogate reports a 3-byte encoded surrogate at the start of s.
func wtf8Surrogate(s string) (uint16, int, bool) {
	if len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2]&0xC0 == 0x80 {
		return uint16(0xD000 | (uint16(s[1]&0x3F) << 6) | uint16(s[2]&0x3F)), 3, true
	}
	return 0, 0, false
}

// fromUnits encodes code units back, pairing surrogates into real characters
// and keeping lone ones as WTF-8.
func fromUnits(u []uint16) string {
	var sb strings.Builder
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF {
			sb.WriteRune(rune(0x10000 + (int(c)-0xD800)<<10 + (int(u[i+1]) - 0xDC00)))
			i++
			continue
		}
		if c >= 0xD800 && c <= 0xDFFF {
			sb.WriteByte(0xED)
			sb.WriteByte(0x80 | byte((c>>6)&0x3F))
			sb.WriteByte(0x80 | byte(c&0x3F))
			continue
		}
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

// finalizeString folds WTF-8 surrogates into proper characters; a surrogate
// that is still unpaired becomes U+FFFD, as Node does when writing UTF-8.
func finalizeString(s string) string {
	if !strings.Contains(s, "\xED") {
		return s
	}
	u := toUnits(s)
	var sb strings.Builder
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF:
			sb.WriteRune(rune(0x10000 + (int(c)-0xD800)<<10 + (int(u[i+1]) - 0xDC00)))
			i++
		case c >= 0xD800 && c <= 0xDFFF:
			sb.WriteRune(0xFFFD)
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

// jsLen is String.prototype.length.
func jsLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if _, w, ok := wtf8Surrogate(s[i:]); ok {
			n++
			i += w
			continue
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
		i += w
	}
	return n
}

// firstUnit/lastUnit are charCodeAt(0) and charCodeAt(length-1); -1 is NaN
// (empty string).
func firstUnit(s string) int {
	if s == "" {
		return -1
	}
	return int(toUnits(s[:firstCharLen(s)])[0])
}

func lastUnit(s string) int {
	if s == "" {
		return -1
	}
	u := toUnits(s[lastCharStart(s):])
	return int(u[len(u)-1])
}

func firstCharLen(s string) int {
	if _, w, ok := wtf8Surrogate(s); ok {
		return w
	}
	_, w := utf8.DecodeRuneInString(s)
	return w
}

func lastCharStart(s string) int {
	if len(s) >= 3 {
		if _, _, ok := wtf8Surrogate(s[len(s)-3:]); ok {
			return len(s) - 3
		}
	}
	_, w := utf8.DecodeLastRuneInString(s)
	return len(s) - w
}

// sliceFirstUnit is s.slice(0, 1); sliceLastUnit is s.slice(-1).
func sliceFirstUnit(s string) string {
	if s == "" {
		return ""
	}
	return fromUnits(toUnits(s[:firstCharLen(s)])[:1])
}

func sliceLastUnit(s string) string {
	if s == "" {
		return ""
	}
	u := toUnits(s[lastCharStart(s):])
	return fromUnits(u[len(u)-1:])
}

// dropFirstUnit is s.slice(1); dropLastUnit is s.slice(0, -1).
func dropFirstUnit(s string) string {
	if s == "" {
		return ""
	}
	n := firstCharLen(s)
	u := toUnits(s[:n])
	return fromUnits(u[1:]) + s[n:]
}

func dropLastUnit(s string) string {
	if s == "" {
		return ""
	}
	i := lastCharStart(s)
	u := toUnits(s[i:])
	return s[:i] + fromUnits(u[:len(u)-1])
}

// ---- character classes (JS regex semantics) ----

// jsSpace is JavaScript's /\s/ for one UTF-16 code unit.
func jsSpace(u int) bool {
	switch u {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return u >= 0x2000 && u <= 0x200A
}

// jsPunctuation is /\p{P}|\p{S}/u applied to a single code unit; a surrogate
// half matches neither.
func jsPunctuation(u int) bool {
	if u < 0 || (u >= 0xD800 && u <= 0xDFFF) {
		return false
	}
	r := rune(u)
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// ---- hast predicates ----

var phrasingTags = setOf(
	"a", "abbr", "area", "b", "bdi", "bdo", "br", "button", "cite", "code",
	"data", "datalist", "del", "dfn", "em", "i", "input", "ins", "kbd",
	"keygen", "label", "map", "mark", "meter", "noscript", "output",
	"progress", "q", "ruby", "s", "samp", "script", "select", "small", "span",
	"strong", "sub", "sup", "template", "textarea", "time", "u", "var", "wbr",
)

var embeddedTags = setOf("audio", "canvas", "embed", "iframe", "img", "math", "object", "picture", "svg", "video")

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}

// hastPhrasing is hast-util-phrasing.
func hastPhrasing(n *hNode) bool {
	if n.kind == hText {
		return true
	}
	if n.kind != hElement {
		return false
	}
	return phrasingTags[n.tag] || embeddedTags[n.tag]
}

// hastWhitespace is hast-util-whitespace for a text node.
func hastWhitespace(n *hNode) bool {
	return n.kind == hText && strings.Trim(n.value, " \t\n\f\r") == ""
}

// ---- hast-util-minify-whitespace ----

var blockTags = setOf(
	"address", "article", "aside", "blockquote", "body", "br", "caption", "center",
	"col", "colgroup", "dd", "dialog", "dir", "div", "dl", "dt", "figcaption",
	"figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "head",
	"header", "hgroup", "hr", "html", "legend", "li", "listing", "main", "menu",
	"nav", "ol", "optgroup", "option", "p", "plaintext", "pre", "section",
	"summary", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "ul", "wbr", "xmp",
)

var contentTags = setOf("button", "input", "select", "textarea")

var skippableTags = setOf(
	"area", "base", "basefont", "dialog", "datalist", "head", "link", "meta",
	"noembed", "noframes", "param", "rp", "script", "source", "style", "template",
	"track", "title",
)

type wsState struct {
	whitespace string
	before     bool
	after      bool
}

type wsResult struct {
	ignore       bool
	stripAtStart bool
	remove       bool
}

var wsRun = regexp.MustCompile(`[\t\n\v\f\r ]+`)

func collapseWS(s string) string { return wsRun.ReplaceAllString(s, " ") }

// minifyWhitespace is minifyWhitespace(tree, {newlines: false}).
func minifyWhitespace(root *hNode) {
	wsMinify(root, wsState{whitespace: "normal"})
}

func wsContent(n *hNode) bool {
	return n != nil && n.kind == hElement && (embeddedTags[n.tag] || contentTags[n.tag])
}

func wsBlocklike(n *hNode) bool { return n.kind == hElement && blockTags[n.tag] }

func wsSkippable(n *hNode) bool {
	return n.kind == hElement && (n.has("hidden") || skippableTags[n.tag])
}

func wsMinify(n *hNode, st wsState) wsResult {
	if hasChildren(n) {
		settings := st
		if n.kind == hRoot || wsBlocklike(n) {
			settings.before = true
			settings.after = true
		}
		settings.whitespace = wsInfer(n, st)
		return wsAll(n, settings)
	}
	if n.kind == hText {
		if st.whitespace == "normal" {
			return wsMinifyText(n, st)
		}
		if st.whitespace == "nowrap" {
			n.value = collapseWS(n.value)
		}
	}
	return wsResult{}
}

func wsMinifyText(n *hNode, st wsState) wsResult {
	value := collapseWS(n.value)
	res := wsResult{}
	start, end := 0, len(value)
	if st.before && start < end && wsRemovable(value[0]) {
		start++
	}
	if start != end && wsRemovable(value[end-1]) {
		if st.after {
			end--
		} else {
			res.stripAtStart = true
		}
	}
	if start == end {
		res.remove = true
	} else {
		n.value = value[start:end]
	}
	return res
}

func wsRemovable(c byte) bool { return c == ' ' || c == '\n' }

func wsAll(parent *hNode, st wsState) wsResult {
	before := st.before
	after := st.after
	length := len(parent.children)
	for index := 0; index < length; index++ {
		child := parent.children[index]
		res := wsMinify(child, wsState{
			whitespace: st.whitespace,
			after:      wsCollapsableAfter(parent.children, index, triOf(after)).orElse(after),
			before:     before,
		})
		if res.remove {
			parent.children = append(parent.children[:index], parent.children[index+1:]...)
			index--
			length--
		} else if !res.ignore {
			before = res.stripAtStart
		}
		if index >= 0 && index < len(parent.children) && wsContent(parent.children[index]) {
			before = false
		}
	}
	return wsResult{stripAtStart: before || after}
}

// tri is a JS boolean-or-undefined.
type tri struct {
	set bool
	val bool
}

func triOf(b bool) tri { return tri{true, b} }

func (t tri) orElse(b bool) bool {
	if t.set {
		return t.val
	}
	return b
}

func wsCollapsableAfter(nodes []*hNode, index int, after tri) tri {
	for index++; index < len(nodes); index++ {
		node := nodes[index]
		result := wsBoundary(node)
		if !result.set && hasChildren(node) && !wsSkippable(node) {
			result = wsCollapsableAfter(node.children, -1, tri{})
		}
		if result.set {
			return result
		}
	}
	return after
}

func wsBoundary(n *hNode) tri {
	switch n.kind {
	case hElement:
		if wsContent(n) {
			return tri{true, false}
		}
		if wsBlocklike(n) {
			return tri{true, true}
		}
	case hText:
		if !hastWhitespace(n) {
			return tri{true, false}
		}
	case hRoot:
		// A root never sits among siblings.
	}
	return tri{}
}

func wsInfer(n *hNode, st wsState) string {
	if n.kind == hElement {
		switch n.tag {
		case "listing", "plaintext", "script", "style", "xmp":
			return "pre"
		case "nobr":
			return "nowrap"
		case "pre":
			if n.has("wrap") {
				return "pre-wrap"
			}
			return "pre"
		case "textarea":
			return "pre-wrap"
		}
	}
	return st.whitespace
}
