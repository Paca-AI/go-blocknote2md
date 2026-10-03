package blocknote2md

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Stage 1: BlockNote blocks -> the HTML BlockNote's external exporter builds
// (serializeBlocksExternalHTML), as a hast tree.

type blockConv struct {
	origin string // window.location.origin equivalent, for annotation cards
}

// blockOut is what a block implementation's toExternalHTML returns: the DOM it
// adds, the element inline content goes into, and the element children go into
// (when it isn't simply "after the block").
type blockOut struct {
	nodes       []*hNode
	content     *hNode
	childrenDOM *hNode
}

func (c *blockConv) blocksToHast(blocks []any) *hNode {
	root := &hNode{kind: hRoot}
	c.serializeBlocks(root, blocks, 0)
	return root
}

func (c *blockConv) serializeBlocks(frag *hNode, blocks []any, nesting int) {
	for _, raw := range blocks {
		if b, ok := raw.(map[string]any); ok {
			c.serializeBlock(frag, b, nesting)
		}
	}
}

func isListEl(n *hNode) bool { return n != nil && n.isEl("ul", "ol") }

func (c *blockConv) serializeBlock(frag *hNode, b map[string]any, nesting int) {
	typ, _ := b["type"].(string)
	props := map[string]any{}
	if p, ok := b["props"].(map[string]any); ok {
		for k, v := range p {
			props[k] = v
		}
	}
	out, ok := c.external(typ, b, props)
	if !ok {
		return
	}

	if out.content != nil && jsTruthy(b["content"]) {
		c.serializeInlineInto(out.content, b["content"], typ == "table")
	}

	listType := ""
	switch typ {
	case "numberedListItem":
		listType = "ol"
	case "bulletListItem", "checkListItem", "toggleListItem":
		listType = "ul"
	}
	if listType != "" {
		last := frag.lastChild()
		if last == nil || last.tag != listType {
			list := hEl(listType)
			if listType == "ol" && jsTruthy(props["start"]) && jsNumber(props["start"]) != 1 {
				list.set("start", jsNumString(props["start"]))
			}
			frag.add(list)
		}
		frag.lastChild().add(out.nodes...)
	} else {
		frag.add(out.nodes...)
	}

	if kids, ok := b["children"].([]any); ok && len(kids) > 0 {
		childFrag := &hNode{kind: hRoot}
		c.serializeBlocks(childFrag, kids, nesting+1)
		if isListEl(frag.lastChild()) {
			for len(childFrag.children) > 0 && isListEl(childFrag.children[0]) {
				li := frag.lastChild().lastChild()
				li.add(childFrag.children[0])
				childFrag.children = childFrag.children[1:]
			}
		}
		switch {
		case out.childrenDOM != nil:
			out.childrenDOM.add(childFrag.children...)
		default:
			frag.add(childFrag.children...)
		}
	}
}

// external is each block's toExternalHTML (or, for blocks without one, the
// render fallback BlockNote uses). ok is false for input BlockNote itself
// rejects.
func (c *blockConv) external(typ string, b map[string]any, props map[string]any) (blockOut, bool) {
	switch typ {
	case "paragraph":
		p := hEl("p")
		return blockOut{nodes: []*hNode{p}, content: p}, true
	case "heading":
		level := any(float64(1))
		if v, ok := props["level"]; ok {
			level = v
		}
		h := hEl("h" + jsNumString(level))
		if jsTruthy(props["isToggleable"]) {
			details := hEl("details").set("open", "")
			details.add(hEl("summary", h))
			return blockOut{nodes: []*hNode{details}, content: h, childrenDOM: details}, true
		}
		return blockOut{nodes: []*hNode{h}, content: h}, true
	case "quote":
		q := hEl("blockquote")
		return blockOut{nodes: []*hNode{q}, content: q}, true
	case "codeBlock":
		lang := "text"
		if v, ok := props["language"]; ok {
			lang = jsString(v)
		}
		code := hEl("code")
		code.classes = strings.Fields("language-" + lang)
		pre := hEl("pre", code)
		return blockOut{nodes: []*hNode{pre}, content: code}, true
	case "divider":
		return blockOut{nodes: []*hNode{hEl("hr")}}, true
	case "bulletListItem", "numberedListItem":
		p := hEl("p")
		return blockOut{nodes: []*hNode{hEl("li", p)}, content: p}, true
	case "checkListItem":
		input := hEl("input").set("type", "checkbox")
		if jsTruthy(props["checked"]) {
			input.set("checked", "")
		}
		p := hEl("p")
		return blockOut{nodes: []*hNode{hEl("li", input, p)}, content: p}, true
	case "toggleListItem":
		p := hEl("p")
		details := hEl("details", hEl("summary", p)).set("open", "")
		return blockOut{nodes: []*hNode{hEl("li", details)}, content: p, childrenDOM: details}, true
	case "table":
		if !validTable(b["content"]) {
			return blockOut{}, false
		}
		table := hEl("table")
		return blockOut{nodes: []*hNode{table}, content: table}, true
	case "image":
		return c.imageLike(props, "img", "Add image", true), true
	case "video":
		return c.imageLike(props, "video", "Add video", false), true
	case "audio":
		return c.imageLike(props, "audio", "Add audio", false), true
	case "file":
		return c.fileBlock(props), true
	case "mermaid":
		code := hEl("code", hTextNode(propString(props, "code", "")))
		code.classes = []string{"language-mermaid"}
		return blockOut{nodes: []*hNode{hEl("pre", code)}}, true
	case "annotationCard":
		path := fmt.Sprintf("/projects/%s/environments/%s/port-forwards/%s/comments/%s",
			propString(props, "projectId", ""), propString(props, "environmentId", ""),
			propString(props, "portForwardId", ""), propString(props, "id", ""))
		a := hEl("a", hTextNode("Comment")).set("href", strings.TrimRight(c.origin, "/")+path)
		return blockOut{nodes: []*hNode{a}}, true
	default:
		// A block type this schema doesn't know: BlockNote would reject it;
		// keep its text so an export never silently drops content.
		p := hEl("p")
		return blockOut{nodes: []*hNode{p}, content: p}, true
	}
}

// imageLike is the image/video/audio toExternalHTML: a placeholder paragraph
// without a URL, the media element (or a link when showPreview is off), and a
// caption wrapper.
func (c *blockConv) imageLike(props map[string]any, tag, placeholder string, isImage bool) blockOut {
	url := propString(props, "url", "")
	name := propString(props, "name", "")
	caption := propString(props, "caption", "")
	if url == "" {
		return blockOut{nodes: []*hNode{hEl("p", hTextNode(placeholder))}}
	}
	showPreview := true
	if v, ok := props["showPreview"]; ok {
		showPreview = jsTruthy(v)
	}
	var el *hNode
	if showPreview {
		el = hEl(tag).set("src", url)
		if isImage {
			alt := name
			if alt == "" {
				alt = caption
			}
			if alt == "" {
				alt = "BlockNote image"
			}
			el.set("alt", alt)
		}
	} else {
		label := name
		if label == "" {
			label = url
		}
		el = hEl("a", hTextNode(label)).set("href", url)
	}
	if caption != "" {
		if showPreview {
			return blockOut{nodes: []*hNode{hEl("figure", el, hEl("figcaption", hTextNode(caption)))}}
		}
		return blockOut{nodes: []*hNode{hEl("div", el, hEl("p", hTextNode(caption)))}}
	}
	return blockOut{nodes: []*hNode{el}}
}

func (c *blockConv) fileBlock(props map[string]any) blockOut {
	url := propString(props, "url", "")
	name := propString(props, "name", "")
	caption := propString(props, "caption", "")
	if url == "" {
		return blockOut{nodes: []*hNode{hEl("p", hTextNode("Add file"))}}
	}
	label := name
	if label == "" {
		label = url
	}
	a := hEl("a", hTextNode(label)).set("href", url)
	if caption != "" {
		return blockOut{nodes: []*hNode{hEl("div", a, hEl("p", hTextNode(caption)))}}
	}
	return blockOut{nodes: []*hNode{a}}
}

// ---- inline content ----

type pmMark struct {
	name string // bold italic underline strike code textColor backgroundColor link
	attr string // href for link, the value for the color styles
}

var markRank = map[string]int{
	"bold": 0, "italic": 1, "underline": 2, "strike": 3, "code": 4,
	"textColor": 5, "backgroundColor": 6, "link": 7,
}

type pmKind int

const (
	pmText pmKind = iota
	pmHardBreak
	pmCustom
)

// pmNode is a ProseMirror inline node as inlineContentToNodes produces it.
type pmNode struct {
	kind   pmKind
	text   string
	marks  []pmMark
	custom string // mention display text
	ctype  string // custom inline content type
}

func sortMarks(m []pmMark) {
	sort.SliceStable(m, func(i, j int) bool { return markRank[m[i].name] < markRank[m[j].name] })
}

// inlineContentToNodes is BlockNote's inlineContentToNodes (blockType unset,
// so "\n" always becomes a hard break).
func inlineContentToNodes(content any) []pmNode {
	var items []any
	switch v := content.(type) {
	case string:
		items = []any{v}
	case []any:
		items = v
	default:
		return nil
	}
	var nodes []pmNode
	for _, item := range items {
		switch it := item.(type) {
		case string:
			nodes = append(nodes, styledTextToNodes(it, nil)...)
		case map[string]any:
			switch it["type"] {
			case "link":
				href := jsString(it["href"])
				nodes = append(nodes, linkToNodes(href, it["content"])...)
			case "text":
				nodes = append(nodes, styledTextToNodes(jsString(it["text"]), it["styles"])...)
			case "rawText":
				nodes = append(nodes, pmNode{kind: pmText, text: normalizeHTMLText(jsString(it["text"]))})
			default:
				if n, ok := customInlineNode(it); ok {
					nodes = append(nodes, n)
				}
			}
		}
	}
	return nodes
}

func linkToNodes(href string, content any) []pmNode {
	var inner []pmNode
	switch c := content.(type) {
	case string:
		inner = styledTextToNodes(c, nil)
	case []any:
		for _, item := range c {
			switch it := item.(type) {
			case string:
				inner = append(inner, styledTextToNodes(it, nil)...)
			case map[string]any:
				inner = append(inner, styledTextToNodes(jsString(it["text"]), it["styles"])...)
			}
		}
	}
	for i := range inner {
		if inner[i].kind == pmText {
			inner[i].marks = append(inner[i].marks, pmMark{name: "link", attr: href})
			sortMarks(inner[i].marks)
		}
	}
	return inner
}

func styledTextToNodes(text string, styles any) []pmNode {
	var marks []pmMark
	if st, ok := styles.(map[string]any); ok {
		for name, v := range st {
			switch name {
			case "bold", "italic", "underline", "strike", "code":
				if jsTruthy(v) {
					marks = append(marks, pmMark{name: name})
				}
			case "textColor", "backgroundColor":
				if jsTruthy(v) {
					marks = append(marks, pmMark{name: name, attr: jsString(v)})
				}
			}
		}
	}
	sortMarks(marks)

	var nodes []pmNode
	// BlockNote splits on "\n" first; what is left of a segment (a CR) is then
	// normalized by the HTML parser, so "a\r\nb" is "a\n", a break, "b".
	for _, seg := range splitKeepNewlines(text) {
		if seg == "\n" {
			nodes = append(nodes, pmNode{kind: pmHardBreak})
			continue
		}
		if seg = normalizeHTMLText(seg); seg == "" {
			continue
		}
		nodes = append(nodes, pmNode{kind: pmText, text: seg, marks: append([]pmMark(nil), marks...)})
	}
	return nodes
}

// splitKeepNewlines is text.split(/(\n)/g).filter(non-empty).
func splitKeepNewlines(s string) []string {
	var out []string
	cur := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > cur {
				out = append(out, s[cur:i])
			}
			out = append(out, "\n")
			cur = i + 1
		}
	}
	if cur < len(s) {
		out = append(out, s[cur:])
	}
	return out
}

// normalizeHTMLText applies what parsing the exported HTML does to text: CR
// LF and lone CR become LF, and NUL is dropped.
func normalizeHTMLText(s string) string {
	if !strings.ContainsAny(s, "\r\x00") {
		return s
	}
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// customInlineNode handles the app's mention inline content; any other custom
// type is unknown to the schema and dropped.
func customInlineNode(it map[string]any) (pmNode, bool) {
	typ, _ := it["type"].(string)
	props, _ := it["props"].(map[string]any)
	switch typ {
	case "teamMention":
		return pmNode{kind: pmCustom, ctype: typ, custom: propString(props, "name", "Unknown")}, true
	case "taskReference", "docReference":
		return pmNode{kind: pmCustom, ctype: typ, custom: propString(props, "title", "Unknown")}, true
	}
	return pmNode{}, false
}

// mentionSpan is the inline content's render output: a span holding an icon
// and the name.
func mentionSpan(name string) *hNode {
	inner := hEl("span", hEl("svg"))
	if name != "" {
		inner.add(hTextNode(normalizeHTMLText(name)))
	}
	return inner
}

func markElement(m pmMark) *hNode {
	switch m.name {
	case "bold":
		return hEl("strong")
	case "italic":
		return hEl("em")
	case "underline":
		return hEl("u")
	case "strike":
		return hEl("s")
	case "code":
		return hEl("code")
	case "link":
		return hEl("a").set("href", m.attr)
	default:
		return hEl("span")
	}
}

// serializeInlineInto is serializeInlineContentExternalHTML. Table cells are
// serialized by ProseMirror's own DOMSerializer instead (table=true), which
// merges adjacent text with equal marks and shares open marks between nodes.
func (c *blockConv) serializeInlineInto(target *hNode, content any, table bool) {
	if table {
		c.serializeTable(target, content)
		return
	}
	for _, n := range inlineContentToNodes(content) {
		switch n.kind {
		case pmCustom:
			outer := hEl("span", mentionSpan(n.custom))
			target.add(outer)
		case pmHardBreak:
			target.add(hEl("br"))
		case pmText:
			dom := hTextNode(n.text)
			for i := len(n.marks) - 1; i >= 0; i-- {
				w := markElement(n.marks[i])
				w.add(dom)
				dom = w
			}
			target.add(dom)
		}
	}
}

// ---- tables ----

func validTable(content any) bool {
	tc, ok := content.(map[string]any)
	if !ok {
		return false
	}
	rows, ok := tc["rows"].([]any)
	if !ok || len(rows) == 0 {
		return false
	}
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			return false
		}
		cells, ok := row["cells"].([]any)
		if !ok || len(cells) == 0 {
			return false
		}
	}
	return true
}

// serializeTable is tableContentToNodes followed by the DOMSerializer.
func (c *blockConv) serializeTable(table *hNode, content any) {
	tc := content.(map[string]any)
	headerRows := int(jsNumber(tc["headerRows"]))
	headerCols := int(jsNumber(tc["headerCols"]))
	for ri, r := range tc["rows"].([]any) {
		tr := hEl("tr")
		for ci, raw := range r.(map[string]any)["cells"].([]any) {
			tag := "td"
			if ri < headerRows || ci < headerCols {
				tag = "th"
			}
			colspan, rowspan := "1", "1"
			var inline any
			switch cell := raw.(type) {
			case nil:
			case string:
				// schema.text(cell): one text node, no hard-break splitting.
				if cell != "" {
					inline = []any{map[string]any{"type": "rawText", "text": cell}}
				}
			case map[string]any:
				if cell["type"] == "tableCell" {
					inline = cell["content"]
					if p, ok := cell["props"].(map[string]any); ok {
						if v, ok := p["colspan"]; ok {
							colspan = jsNumString(v)
						}
						if v, ok := p["rowspan"]; ok {
							rowspan = jsNumString(v)
						}
					}
				}
			case []any:
				inline = cell
			}
			p := hEl("p")
			pmSerialize(p, mergeText(inlineContentToNodes(inline)))
			td := hEl(tag, p).set("colSpan", colspan).set("rowSpan", rowspan)
			tr.add(td)
		}
		table.add(tr)
	}
}

func sameMarks(a, b []pmMark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeText is Fragment.fromArray: adjacent text nodes with equal marks join.
func mergeText(nodes []pmNode) []pmNode {
	var out []pmNode
	for _, n := range nodes {
		if n.kind == pmText && len(out) > 0 {
			prev := &out[len(out)-1]
			if prev.kind == pmText && sameMarks(prev.marks, n.marks) {
				prev.text += n.text
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// pmSerialize is prosemirror-model's DOMSerializer.serializeFragment: marks
// stay open across neighbouring nodes while they remain equal.
func pmSerialize(target *hNode, nodes []pmNode) {
	type activeMark struct {
		mark   pmMark
		parent *hNode
	}
	top := target
	var active []activeMark
	for _, n := range nodes {
		if len(active) > 0 || len(n.marks) > 0 {
			keep, rendered := 0, 0
			for keep < len(active) && rendered < len(n.marks) {
				if n.marks[rendered] != active[keep].mark {
					break
				}
				keep++
				rendered++
			}
			for keep < len(active) {
				top = active[len(active)-1].parent
				active = active[:len(active)-1]
			}
			for rendered < len(n.marks) {
				add := n.marks[rendered]
				rendered++
				dom := markElement(add)
				active = append(active, activeMark{add, top})
				top.add(dom)
				top = dom
			}
		}
		switch n.kind {
		case pmText:
			top.add(hTextNode(n.text))
		case pmHardBreak:
			top.add(hEl("br"))
		case pmCustom:
			top.add(mentionSpan(n.custom))
		}
	}
}

// ---- JS-flavoured value helpers ----

func jsTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

func jsNumber(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
			return f
		}
	}
	return 0
}

// jsString is String(v) for the JSON value types props can hold.
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return jsNumString(x)
	case bool:
		return strconv.FormatBool(x)
	}
	return fmt.Sprint(v)
}

func jsNumString(v any) string {
	f, ok := v.(float64)
	if !ok {
		return jsString(v)
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// propString reads a string prop, with the block's default when it is absent.
func propString(props map[string]any, key, def string) string {
	v, ok := props[key]
	if !ok || v == nil {
		return def
	}
	return jsString(v)
}
