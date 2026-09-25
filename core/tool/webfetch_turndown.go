package tool

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/net/html"
)

// This file is a Go port of turndown 7.2.0 (https://github.com/mixmark-io/turndown),
// the library webfetch uses to convert HTML to markdown. It implements the default
// rule set (including the keep/remove filters) and the option overrides webfetch
// applies.

var turndownBlockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "audio": true, "blockquote": true,
	"body": true, "canvas": true, "center": true, "dd": true, "dir": true, "div": true,
	"dl": true, "dt": true, "fieldset": true, "figcaption": true, "figure": true,
	"footer": true, "form": true, "frameset": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "header": true, "hgroup": true, "hr": true,
	"html": true, "isindex": true, "li": true, "main": true, "menu": true, "nav": true,
	"noframes": true, "noscript": true, "ol": true, "output": true, "p": true,
	"pre": true, "section": true, "table": true, "tbody": true, "td": true,
	"tfoot": true, "th": true, "thead": true, "tr": true, "ul": true,
}

var turndownVoidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "command": true, "embed": true,
	"hr": true, "img": true, "input": true, "keygen": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

var turndownMeaningfulWhenBlankElements = map[string]bool{
	"a": true, "table": true, "thead": true, "tbody": true, "tfoot": true, "th": true,
	"td": true, "iframe": true, "script": true, "audio": true, "video": true,
}

type turndownOptions struct {
	headingStyle       string
	hr                 string
	bulletListMarker   string
	codeBlockStyle     string
	fence              string
	emDelimiter        string
	strongDelimiter    string
	linkStyle          string
	linkReferenceStyle string
	br                 string
	preformattedCode   bool
}

func defaultTurndownOptions() turndownOptions {
	return turndownOptions{
		headingStyle:       "setext",
		hr:                 "* * *",
		bulletListMarker:   "*",
		codeBlockStyle:     "indented",
		fence:              "```",
		emDelimiter:        "_",
		strongDelimiter:    "**",
		linkStyle:          "inlined",
		linkReferenceStyle: "full",
		br:                 "  ",
	}
}

type turndownRule struct {
	tags        []string
	filter      func(node *html.Node, options *turndownOptions) bool
	replacement func(content string, node *html.Node, options *turndownOptions, state *turndownState) string
	appendFn    func(options *turndownOptions, state *turndownState) string
}

func (r *turndownRule) matches(node *html.Node, options *turndownOptions) bool {
	if len(r.tags) > 0 {
		name := strings.ToLower(node.Data)
		for _, tag := range r.tags {
			if tag == name {
				return true
			}
		}
		return false
	}
	if r.filter != nil {
		return r.filter(node, options)
	}
	return false
}

type turndownState struct {
	options    turndownOptions
	rules      []turndownRule
	keep       []turndownRule
	remove     []turndownRule
	references []string
}

type turndownNode struct {
	node     *html.Node
	isBlock  bool
	isCode   bool
	isBlank  bool
	leading  string
	trailing string
}

func newTurndownNode(node *html.Node, options *turndownOptions) *turndownNode {
	n := &turndownNode{node: node}
	n.isBlock = turndownIsBlock(node)
	n.isCode = turndownIsCode(node)
	n.isBlank = turndownIsBlank(node)
	n.leading, n.trailing = turndownFlankingWhitespace(node, options)
	return n
}

func (s *turndownState) ruleForNode(n *turndownNode) *turndownRule {
	if n.isBlank {
		return &turndownRule{replacement: turndownBlankReplacement}
	}
	for i := range s.rules {
		if s.rules[i].matches(n.node, &s.options) {
			return &s.rules[i]
		}
	}
	for i := range s.keep {
		if s.keep[i].matches(n.node, &s.options) {
			return &s.keep[i]
		}
	}
	for i := range s.remove {
		if s.remove[i].matches(n.node, &s.options) {
			return &s.remove[i]
		}
	}
	return &turndownRule{replacement: turndownDefaultReplacement}
}

func (s *turndownState) process(parent *html.Node) string {
	output := ""
	for child := parent.FirstChild; child != nil; child = child.NextSibling {
		n := newTurndownNode(child, &s.options)
		replacement := ""
		if child.Type == html.TextNode || child.Type == html.RawNode {
			if n.isCode {
				replacement = child.Data
			} else {
				replacement = turndownEscape(child.Data)
			}
		} else if child.Type == html.ElementNode {
			replacement = s.replacementForNode(n)
		}
		output = turndownJoin(output, replacement)
	}
	return output
}

func (s *turndownState) replacementForNode(n *turndownNode) string {
	rule := s.ruleForNode(n)
	content := s.process(n.node)
	leading, trailing := n.leading, n.trailing
	if leading != "" || trailing != "" {
		content = turndownTrimSpace(content)
	}
	return leading + rule.replacement(content, n.node, &s.options, s) + trailing
}

func (s *turndownState) postProcess(output string) string {
	for i := range s.rules {
		if s.rules[i].appendFn != nil {
			output = turndownJoin(output, s.rules[i].appendFn(&s.options, s))
		}
	}
	output = strings.TrimLeft(output, "\t\r\n")
	return strings.TrimRightFunc(output, turndownIsWhitespaceRune)
}

// turndownService is the converter entry point.
type turndownService struct {
	options turndownOptions
	rules   []turndownRule
	keep    []turndownRule
	remove  []turndownRule
}

func newTurndownService(options turndownOptions) *turndownService {
	s := &turndownService{options: options}
	s.rules = turndownDefaultRules()
	return s
}

func (s *turndownService) removeTags(tags ...string) {
	for _, tag := range tags {
		s.remove = append([]turndownRule{{
			tags:        []string{tag},
			replacement: func(string, *html.Node, *turndownOptions, *turndownState) string { return "" },
		}}, s.remove...)
	}
}

func (s *turndownService) turndown(input string) (string, error) {
	if input == "" {
		return "", nil
	}
	root, err := turndownRootNode(input, &s.options)
	if err != nil {
		return "", err
	}
	state := &turndownState{options: s.options, rules: s.rules, keep: s.keep, remove: s.remove}
	return state.postProcess(state.process(root)), nil
}

func turndownDefaultRules() []turndownRule {
	return []turndownRule{
		turndownRuleParagraph(),
		turndownRuleBreak(),
		turndownRuleHeadings(),
		turndownRuleBlockquote(),
		turndownRuleList(),
		turndownRuleListItem(),
		turndownRuleIndentedCode(),
		turndownRuleFencedCode(),
		turndownRuleHorizontalRule(),
		turndownRuleInlinedLink(),
		turndownRuleReferencedLink(),
		turndownRuleEmphasis(),
		turndownRuleStrong(),
		turndownRuleInlineCode(),
		turndownRuleImage(),
	}
}

func turndownRuleParagraph() turndownRule {
	return turndownRule{
		tags: []string{"p"},
		replacement: func(content string, _ *html.Node, _ *turndownOptions, _ *turndownState) string {
			return "\n\n" + content + "\n\n"
		},
	}
}

func turndownRuleBreak() turndownRule {
	return turndownRule{
		tags: []string{"br"},
		replacement: func(_ string, _ *html.Node, options *turndownOptions, _ *turndownState) string {
			return options.br + "\n"
		},
	}
}

func turndownRuleHeadings() turndownRule {
	return turndownRule{
		tags: []string{"h1", "h2", "h3", "h4", "h5", "h6"},
		replacement: func(content string, node *html.Node, options *turndownOptions, _ *turndownState) string {
			level := int(node.Data[1] - '0')
			if options.headingStyle == "setext" && level < 3 {
				char := "-"
				if level == 1 {
					char = "="
				}
				return "\n\n" + content + "\n" + strings.Repeat(char, turndownJSLength(content)) + "\n\n"
			}
			return "\n\n" + strings.Repeat("#", level) + " " + content + "\n\n"
		},
	}
}

func turndownRuleBlockquote() turndownRule {
	return turndownRule{
		tags: []string{"blockquote"},
		replacement: func(content string, _ *html.Node, _ *turndownOptions, _ *turndownState) string {
			content = turndownBlockquoteTrim.ReplaceAllString(content, "")
			content = turndownLineStart.ReplaceAllString(content, "> ")
			return "\n\n" + content + "\n\n"
		},
	}
}

func turndownRuleList() turndownRule {
	return turndownRule{
		tags: []string{"ul", "ol"},
		replacement: func(content string, node *html.Node, _ *turndownOptions, _ *turndownState) string {
			parent := node.Parent
			if parent != nil && parent.Type == html.ElementNode &&
				strings.ToLower(parent.Data) == "li" && turndownLastElementChild(parent) == node {
				return "\n" + content
			}
			return "\n\n" + content + "\n\n"
		},
	}
}

func turndownRuleListItem() turndownRule {
	return turndownRule{
		tags: []string{"li"},
		replacement: func(content string, node *html.Node, options *turndownOptions, _ *turndownState) string {
			content = turndownLeadingNewlines.ReplaceAllString(content, "")
			content = turndownTrailingNewlinesRe.ReplaceAllString(content, "\n")
			content = turndownNewlines.ReplaceAllString(content, "\n    ")
			prefix := options.bulletListMarker + "   "
			parent := node.Parent
			if parent != nil && parent.Type == html.ElementNode && strings.ToLower(parent.Data) == "ol" {
				start := turndownAttr(parent, "start")
				index := turndownElementIndex(parent, node)
				if start != "" {
					base, err := strconv.ParseFloat(start, 64)
					if err != nil {
						base = math.NaN()
					}
					prefix = strconv.FormatFloat(base+float64(index), 'f', -1, 64) + ".  "
				} else {
					prefix = strconv.Itoa(index+1) + ".  "
				}
			}
			result := prefix + content
			if node.NextSibling != nil && !strings.HasSuffix(content, "\n") {
				result += "\n"
			}
			return result
		},
	}
}

func turndownRuleIndentedCode() turndownRule {
	return turndownRule{
		filter: func(node *html.Node, options *turndownOptions) bool {
			return options.codeBlockStyle == "indented" &&
				strings.ToLower(node.Data) == "pre" &&
				node.FirstChild != nil && node.FirstChild.Type == html.ElementNode &&
				strings.ToLower(node.FirstChild.Data) == "code"
		},
		replacement: func(_ string, node *html.Node, _ *turndownOptions, _ *turndownState) string {
			return "\n\n    " + turndownNewlines.ReplaceAllString(turndownTextContent(node.FirstChild), "\n    ") + "\n\n"
		},
	}
}

func turndownRuleFencedCode() turndownRule {
	return turndownRule{
		filter: func(node *html.Node, options *turndownOptions) bool {
			return options.codeBlockStyle == "fenced" &&
				strings.ToLower(node.Data) == "pre" &&
				node.FirstChild != nil && node.FirstChild.Type == html.ElementNode &&
				strings.ToLower(node.FirstChild.Data) == "code"
		},
		replacement: func(_ string, node *html.Node, options *turndownOptions, _ *turndownState) string {
			className := turndownAttr(node.FirstChild, "class")
			language := ""
			if m := turndownLanguageRe.FindStringSubmatch(className); m != nil {
				language = m[1]
			}
			code := turndownTextContent(node.FirstChild)

			fenceChar := ""
			if options.fence != "" {
				fenceChar = options.fence[:1]
			}
			fenceSize := 3
			fenceRe := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(fenceChar) + "{3,}")
			for _, match := range fenceRe.FindAllString(code, -1) {
				if len(match) >= fenceSize {
					fenceSize = len(match) + 1
				}
			}
			fence := strings.Repeat(fenceChar, fenceSize)
			code = turndownTrailingNewline.ReplaceAllString(code, "")
			return "\n\n" + fence + language + "\n" + code + "\n" + fence + "\n\n"
		},
	}
}

func turndownRuleHorizontalRule() turndownRule {
	return turndownRule{
		tags: []string{"hr"},
		replacement: func(_ string, _ *html.Node, options *turndownOptions, _ *turndownState) string {
			return "\n\n" + options.hr + "\n\n"
		},
	}
}

func turndownRuleInlinedLink() turndownRule {
	return turndownRule{
		filter: func(node *html.Node, options *turndownOptions) bool {
			return options.linkStyle == "inlined" && strings.ToLower(node.Data) == "a" && turndownAttr(node, "href") != ""
		},
		replacement: func(content string, node *html.Node, _ *turndownOptions, _ *turndownState) string {
			href := turndownAttr(node, "href")
			href = turndownLinkParens.ReplaceAllString(href, "\\$1")
			title := turndownCleanAttribute(turndownAttr(node, "title"))
			if title != "" {
				title = ` "` + turndownQuote.ReplaceAllString(title, `\"`) + `"`
			}
			return "[" + content + "](" + href + title + ")"
		},
	}
}

func turndownRuleReferencedLink() turndownRule {
	return turndownRule{
		filter: func(node *html.Node, options *turndownOptions) bool {
			return options.linkStyle == "referenced" && strings.ToLower(node.Data) == "a" && turndownAttr(node, "href") != ""
		},
		replacement: func(content string, node *html.Node, options *turndownOptions, state *turndownState) string {
			href := turndownAttr(node, "href")
			title := turndownCleanAttribute(turndownAttr(node, "title"))
			if title != "" {
				title = ` "` + title + `"`
			}
			var replacement, reference string
			switch options.linkReferenceStyle {
			case "collapsed":
				replacement = "[" + content + "][]"
				reference = "[" + content + "]: " + href + title
			case "shortcut":
				replacement = "[" + content + "]"
				reference = "[" + content + "]: " + href + title
			default:
				id := len(state.references) + 1
				replacement = "[" + content + "][" + strconv.Itoa(id) + "]"
				reference = "[" + strconv.Itoa(id) + "]: " + href + title
			}
			state.references = append(state.references, reference)
			return replacement
		},
		appendFn: func(_ *turndownOptions, state *turndownState) string {
			if len(state.references) == 0 {
				return ""
			}
			references := "\n\n" + strings.Join(state.references, "\n") + "\n\n"
			state.references = nil
			return references
		},
	}
}

func turndownRuleEmphasis() turndownRule {
	return turndownRule{
		tags: []string{"em", "i"},
		replacement: func(content string, _ *html.Node, options *turndownOptions, _ *turndownState) string {
			if turndownTrimSpace(content) == "" {
				return ""
			}
			return options.emDelimiter + content + options.emDelimiter
		},
	}
}

func turndownRuleStrong() turndownRule {
	return turndownRule{
		tags: []string{"strong", "b"},
		replacement: func(content string, _ *html.Node, options *turndownOptions, _ *turndownState) string {
			if turndownTrimSpace(content) == "" {
				return ""
			}
			return options.strongDelimiter + content + options.strongDelimiter
		},
	}
}

func turndownRuleInlineCode() turndownRule {
	return turndownRule{
		filter: func(node *html.Node, _ *turndownOptions) bool {
			hasSiblings := node.PrevSibling != nil || node.NextSibling != nil
			isCodeBlock := node.Parent != nil && node.Parent.Type == html.ElementNode &&
				strings.ToLower(node.Parent.Data) == "pre" && !hasSiblings
			return strings.ToLower(node.Data) == "code" && !isCodeBlock
		},
		replacement: func(content string, _ *html.Node, _ *turndownOptions, _ *turndownState) string {
			if content == "" {
				return ""
			}
			content = turndownLineBreaks.ReplaceAllString(content, " ")
			extraSpace := ""
			if turndownCodeSpaceRe.MatchString(content) {
				extraSpace = " "
			}
			delimiter := "`"
			matches := turndownBackticks.FindAllString(content, -1)
			for turndownContains(matches, delimiter) {
				delimiter += "`"
			}
			return delimiter + extraSpace + content + extraSpace + delimiter
		},
	}
}

func turndownRuleImage() turndownRule {
	return turndownRule{
		tags: []string{"img"},
		replacement: func(_ string, node *html.Node, _ *turndownOptions, _ *turndownState) string {
			alt := turndownCleanAttribute(turndownAttr(node, "alt"))
			src := turndownAttr(node, "src")
			title := turndownCleanAttribute(turndownAttr(node, "title"))
			titlePart := ""
			if title != "" {
				titlePart = ` "` + title + `"`
			}
			if src == "" {
				return ""
			}
			return "![" + alt + "](" + src + titlePart + ")"
		},
	}
}

func turndownBlankReplacement(_ string, node *html.Node, _ *turndownOptions, _ *turndownState) string {
	if turndownIsBlock(node) {
		return "\n\n"
	}
	return ""
}

func turndownDefaultReplacement(content string, node *html.Node, _ *turndownOptions, _ *turndownState) string {
	if turndownIsBlock(node) {
		return "\n\n" + content + "\n\n"
	}
	return content
}

func turndownRootNode(input string, options *turndownOptions) (*html.Node, error) {
	context := &html.Node{Type: html.ElementNode, Data: "x-turndown"}
	nodes, err := html.ParseFragment(strings.NewReader(input), context)
	if err != nil {
		return nil, err
	}
	root := &html.Node{Type: html.ElementNode, Data: "x-turndown"}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	turndownCollapseWhitespace(root, turndownIsPre)
	return root, nil
}

func turndownIsPre(node *html.Node) bool {
	return node.Type == html.ElementNode && strings.ToLower(node.Data) == "pre"
}

// turndownWhitespaceState carries the running state of whitespace collapsing.
type turndownWhitespaceState struct {
	isPre         func(*html.Node) bool
	prevText      *html.Node
	keepLeadingWs bool
	prev          *html.Node
}

// turndownCollapseWhitespace is adapted from collapse-whitespace by Luc Thevenard.
func turndownCollapseWhitespace(root *html.Node, isPre func(*html.Node) bool) {
	if root.FirstChild == nil || isPre(root) {
		return
	}
	state := &turndownWhitespaceState{isPre: isPre}
	node := turndownNext(state.prev, root, isPre)
	for node != root {
		if next, removed := state.visit(node); removed {
			node = next
			continue
		}
		nextNode := turndownNext(state.prev, node, isPre)
		state.prev = node
		node = nextNode
	}
	if state.prevText != nil {
		state.prevText.Data = strings.TrimSuffix(state.prevText.Data, " ")
		if state.prevText.Data == "" {
			turndownRemove(state.prevText)
		}
	}
}

// visit collapses one node, reporting removed=true with the next node when the
// node was dropped.
func (s *turndownWhitespaceState) visit(node *html.Node) (*html.Node, bool) {
	switch node.Type {
	case html.TextNode, html.RawNode:
		text := turndownWhitespaceRun.ReplaceAllString(node.Data, " ")
		if (s.prevText == nil || strings.HasSuffix(s.prevText.Data, " ")) && !s.keepLeadingWs && strings.HasPrefix(text, " ") {
			text = text[1:]
		}
		if text == "" {
			return turndownRemove(node), true
		}
		node.Data = text
		s.prevText = node
		return nil, false
	case html.ElementNode:
		s.collapseElement(node)
		return nil, false
	default:
		return turndownRemove(node), true
	}
}

func (s *turndownWhitespaceState) collapseElement(node *html.Node) {
	if turndownIsBlock(node) || strings.ToLower(node.Data) == "br" {
		if s.prevText != nil {
			s.prevText.Data = strings.TrimSuffix(s.prevText.Data, " ")
		}
		s.prevText = nil
		s.keepLeadingWs = false
	} else if turndownIsVoid(node) || s.isPre(node) {
		s.prevText = nil
		s.keepLeadingWs = true
	} else if s.prevText != nil {
		s.keepLeadingWs = false
	}
}

func turndownRemove(node *html.Node) *html.Node {
	next := node.Parent
	if node.NextSibling != nil {
		next = node.NextSibling
	}
	if node.Parent != nil {
		node.Parent.RemoveChild(node)
	}
	return next
}

func turndownNext(prev, current *html.Node, isPre func(*html.Node) bool) *html.Node {
	if (prev != nil && prev.Parent == current) || isPre(current) {
		if current.NextSibling != nil {
			return current.NextSibling
		}
		return current.Parent
	}
	if current.FirstChild != nil {
		return current.FirstChild
	}
	if current.NextSibling != nil {
		return current.NextSibling
	}
	return current.Parent
}

func turndownJoin(output, replacement string) string {
	s1 := turndownTrimTrailingNewlines(output)
	s2 := turndownTrimLeadingNewlines(replacement)
	nls := len(output) - len(s1)
	if l := len(replacement) - len(s2); l > nls {
		nls = l
	}
	if nls > 2 {
		nls = 2
	}
	return s1 + "\n\n"[:nls] + s2
}

func turndownTrimLeadingNewlines(s string) string {
	return turndownLeadingNewlines.ReplaceAllString(s, "")
}

func turndownTrimTrailingNewlines(s string) string {
	end := len(s)
	for end > 0 && s[end-1] == '\n' {
		end--
	}
	return s[:end]
}

func turndownEscape(s string) string {
	for _, escape := range turndownEscapes {
		s = escape.re.ReplaceAllString(s, escape.replacement)
	}
	return s
}

var turndownEscapes = []struct {
	re          *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`\\`), "\\\\"},
	{regexp.MustCompile(`\*`), "\\*"},
	{regexp.MustCompile(`^-`), "\\-"},
	{regexp.MustCompile(`^\+ `), "\\+ "},
	{regexp.MustCompile(`^(=+)`), "\\$1"},
	{regexp.MustCompile(`^(#{1,6}) `), "\\$1 "},
	{regexp.MustCompile("`"), "\\\x60"},
	{regexp.MustCompile(`^~~~`), "\\~~~"},
	{regexp.MustCompile(`\[`), "\\["},
	{regexp.MustCompile(`\]`), "\\]"},
	{regexp.MustCompile(`^>`), "\\>"},
	{regexp.MustCompile(`_`), "\\_"},
	{regexp.MustCompile(`^(\d+)\. `), "$1\\. "},
}

func turndownIsBlock(node *html.Node) bool {
	return node.Type == html.ElementNode && turndownBlockElements[strings.ToLower(node.Data)]
}

func turndownIsVoid(node *html.Node) bool {
	return node.Type == html.ElementNode && turndownVoidElements[strings.ToLower(node.Data)]
}

func turndownIsMeaningfulWhenBlank(node *html.Node) bool {
	return node.Type == html.ElementNode && turndownMeaningfulWhenBlankElements[strings.ToLower(node.Data)]
}

func turndownHas(node *html.Node, set map[string]bool) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && set[strings.ToLower(child.Data)] {
			return true
		}
		if turndownHas(child, set) {
			return true
		}
	}
	return false
}

func turndownIsCode(node *html.Node) bool {
	for current := node; current != nil; current = current.Parent {
		if current.Type == html.ElementNode && strings.ToLower(current.Data) == "code" {
			return true
		}
	}
	return false
}

func turndownIsBlank(node *html.Node) bool {
	return !turndownIsVoid(node) &&
		!turndownIsMeaningfulWhenBlank(node) &&
		turndownIsWhitespaceOnly(turndownTextContent(node)) &&
		!turndownHas(node, turndownVoidElements) &&
		!turndownHas(node, turndownMeaningfulWhenBlankElements)
}

func turndownTextContent(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode || n.Type == html.RawNode {
			b.WriteString(n.Data)
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

type turndownEdges struct {
	leading          string
	leadingAscii     string
	leadingNonAscii  string
	trailing         string
	trailingNonAscii string
	trailingAscii    string
}

func turndownEdgeWhitespace(s string) turndownEdges {
	runes := []rune(s)
	leadLen := 0
	for leadLen < len(runes) && turndownIsWhitespaceRune(runes[leadLen]) {
		leadLen++
	}
	if leadLen == len(runes) {
		leading := s
		ascii := 0
		for ascii < len(runes) && turndownIsASCIIWhitespaceRune(runes[ascii]) {
			ascii++
		}
		return turndownEdges{
			leading:         leading,
			leadingAscii:    string(runes[:ascii]),
			leadingNonAscii: string(runes[ascii:]),
		}
	}

	trailStart := len(runes)
	for trailStart > 0 && turndownIsWhitespaceRune(runes[trailStart-1]) {
		trailStart--
	}
	trailing := string(runes[trailStart:])
	trailingRunes := runes[trailStart:]
	asciiSuffix := 0
	for asciiSuffix < len(trailingRunes) && turndownIsASCIIWhitespaceRune(trailingRunes[len(trailingRunes)-1-asciiSuffix]) {
		asciiSuffix++
	}
	trailingAscii := string(trailingRunes[len(trailingRunes)-asciiSuffix:])
	trailingNonAscii := string(trailingRunes[:len(trailingRunes)-asciiSuffix])

	leadingRunes := runes[:leadLen]
	leadingAsciiLen := 0
	for leadingAsciiLen < len(leadingRunes) && turndownIsASCIIWhitespaceRune(leadingRunes[leadingAsciiLen]) {
		leadingAsciiLen++
	}

	return turndownEdges{
		leading:          string(leadingRunes),
		leadingAscii:     string(leadingRunes[:leadingAsciiLen]),
		leadingNonAscii:  string(leadingRunes[leadingAsciiLen:]),
		trailing:         trailing,
		trailingNonAscii: trailingNonAscii,
		trailingAscii:    trailingAscii,
	}
}

func turndownFlankingWhitespace(node *html.Node, options *turndownOptions) (string, string) {
	if turndownIsBlock(node) || (options.preformattedCode && turndownIsCode(node)) {
		return "", ""
	}
	edges := turndownEdgeWhitespace(turndownTextContent(node))
	leading := edges.leading
	trailing := edges.trailing
	if edges.leadingAscii != "" && turndownIsFlankedByWhitespace("left", node, options) {
		leading = edges.leadingNonAscii
	}
	if edges.trailingAscii != "" && turndownIsFlankedByWhitespace("right", node, options) {
		trailing = edges.trailingNonAscii
	}
	return leading, trailing
}

func turndownIsFlankedByWhitespace(side string, node *html.Node, options *turndownOptions) bool {
	var sibling *html.Node
	if side == "left" {
		sibling = node.PrevSibling
	} else {
		sibling = node.NextSibling
	}
	if sibling == nil {
		return false
	}
	hasSpace := func(value string) bool {
		if side == "left" {
			return strings.HasSuffix(value, " ")
		}
		return strings.HasPrefix(value, " ")
	}
	if sibling.Type == html.TextNode || sibling.Type == html.RawNode {
		return hasSpace(sibling.Data)
	}
	if options.preformattedCode && sibling.Type == html.ElementNode && strings.ToLower(sibling.Data) == "code" {
		return false
	}
	if sibling.Type == html.ElementNode && !turndownIsBlock(sibling) {
		return hasSpace(turndownTextContent(sibling))
	}
	return false
}

func turndownCleanAttribute(attribute string) string {
	if attribute == "" {
		return ""
	}
	return turndownAttributeNewlines.ReplaceAllString(attribute, "\n")
}

func turndownAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func turndownLastElementChild(node *html.Node) *html.Node {
	var last *html.Node
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode {
			last = child
		}
	}
	return last
}

func turndownElementIndex(parent, node *html.Node) int {
	index := 0
	for child := parent.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		if child == node {
			return index
		}
		index++
	}
	return index
}

func turndownContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// turndownIsWhitespaceRune matches the JavaScript \s class: WhiteSpace plus
// LineTerminator (notably it excludes U+0085 NEL and includes U+FEFF).
func turndownIsWhitespaceRune(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// turndownTrimSpace trims the whitespace JavaScript's trim would, which differs
// from strings.TrimSpace (unicode.IsSpace).
func turndownTrimSpace(s string) string {
	return strings.TrimFunc(s, turndownIsWhitespaceRune)
}

// turndownJSLength is the string length in UTF-16 code units.
func turndownJSLength(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func turndownIsASCIIWhitespaceRune(r rune) bool {
	switch r {
	case ' ', '\t', '\r', '\n':
		return true
	}
	return false
}

func turndownIsWhitespaceOnly(s string) bool {
	for _, r := range s {
		if !turndownIsWhitespaceRune(r) {
			return false
		}
	}
	return true
}

var (
	turndownLeadingNewlines    = regexp.MustCompile(`^\n*`)
	turndownTrailingNewlinesRe = regexp.MustCompile(`\n+$`)
	turndownTrailingNewline    = regexp.MustCompile(`\n$`)
	turndownNewlines           = regexp.MustCompile(`\n`)
	turndownBlockquoteTrim     = regexp.MustCompile(`^\n+|\n+$`)
	turndownLineStart          = regexp.MustCompile(`(?m)^`)
	turndownLanguageRe         = regexp.MustCompile(`language-(\S+)`)
	turndownLinkParens         = regexp.MustCompile(`([()])`)
	turndownQuote              = regexp.MustCompile(`"`)
	turndownLineBreaks         = regexp.MustCompile(`\r?\n|\r`)
	turndownCodeSpaceRe        = regexp.MustCompile("^`|^ .*?[^ ].* $|`$")
	turndownBackticks          = regexp.MustCompile("`+")
	turndownWhitespaceRun      = regexp.MustCompile(`[ \r\n\t]+`)
	turndownAttributeNewlines  = regexp.MustCompile(`(\n+[\t\n\v\f\r\p{Zs}\x{2028}\x{2029}\x{FEFF}]*)+`)
)
