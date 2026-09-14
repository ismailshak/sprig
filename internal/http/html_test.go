package http

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// element is an HTML element read from a response body: its tag name, its
// attributes, and its children in document order. Tests read a page through it
// instead of matching markup, so a class or a wrapper element can change
// without failing a test about something else.
type element struct {
	tag   string
	attrs map[string]string
	// children holds each child element as an *element and each run of text
	// as a string.
	children []any
}

// readHTML parses a whole page or a fragment into an element with no tag that
// holds everything in body. The decoder's HTML settings accept void elements
// such as input, HTML entities, and attributes with no value.
//
// The decoder closes a void element only when it reads the next tag, so body
// is wrapped in one more element in case it ends with one. readHTML panics on markup the decoder cannot read, because the templates are
// compiled into the binary and markup that does not parse is a broken build.
func readHTML(body string) *element {
	decoder := xml.NewDecoder(strings.NewReader("<readhtml>" + body + "</readhtml>"))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity

	root := &element{}
	open := []*element{root}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			panic(fmt.Sprintf("reading HTML: %v\n%s", err, body))
		}
		parent := open[len(open)-1]
		switch token := token.(type) {
		case xml.StartElement:
			child := &element{tag: token.Name.Local, attrs: map[string]string{}}
			for _, a := range token.Attr {
				name := a.Name.Local
				if a.Name.Space != "" {
					name = a.Name.Space + ":" + name
				}
				child.attrs[name] = a.Value
			}
			parent.children = append(parent.children, child)
			open = append(open, child)
		case xml.EndElement:
			if len(open) > 1 {
				open = open[:len(open)-1]
			}
		case xml.CharData:
			parent.children = append(parent.children, string(token))
		}
	}

	wrapper := root.first(isTag("readhtml"))
	wrapper.tag = ""
	return wrapper
}

// all returns every element below e that matches every predicate, in document
// order. It returns nil when e is nil.
func (e *element) all(match ...func(*element) bool) []*element {
	if e == nil {
		return nil
	}
	var found []*element
	for _, c := range e.children {
		child, ok := c.(*element)
		if !ok {
			continue
		}
		if !slices.ContainsFunc(match, func(m func(*element) bool) bool { return !m(child) }) {
			found = append(found, child)
		}
		found = append(found, child.all(match...)...)
	}
	return found
}

// first returns the first element below e that matches every predicate, or
// nil.
func (e *element) first(match ...func(*element) bool) *element {
	if found := e.all(match...); len(found) > 0 {
		return found[0]
	}
	return nil
}

// byID returns the element below e whose id is id, or nil.
func (e *element) byID(id string) *element {
	return e.first(attrIs("id", id))
}

// attr returns the value of the attribute name. It is empty when e is nil or
// has no such attribute. An attribute written with no value, such as checked,
// has its own name as its value.
func (e *element) attr(name string) string {
	if e == nil {
		return ""
	}
	return e.attrs[name]
}

// has reports whether e has the attribute name, such as checked or selected.
func (e *element) has(name string) bool {
	if e == nil {
		return false
	}
	_, ok := e.attrs[name]
	return ok
}

// text returns the text a person reads in e. A space goes where an element
// starts or ends, each run of spaces, tabs and newlines becomes one space, and
// the ends are trimmed. A no-break space is kept, because the templates use
// one where a line must not break.
func (e *element) text() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	e.writeText(&b)
	return strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	}), " ")
}

func (e *element) writeText(b *strings.Builder) {
	for _, c := range e.children {
		switch c := c.(type) {
		case string:
			b.WriteString(c)
		case *element:
			b.WriteByte(' ')
			c.writeText(b)
			b.WriteByte(' ')
		}
	}
}

// String renders e as HTML with its attributes sorted, for a failure message.
func (e *element) String() string {
	if e == nil {
		return "<nil>"
	}
	var b strings.Builder
	e.writeHTML(&b)
	return b.String()
}

func (e *element) writeHTML(b *strings.Builder) {
	if e.tag != "" {
		b.WriteString("<" + e.tag)
		for _, name := range slices.Sorted(maps.Keys(e.attrs)) {
			fmt.Fprintf(b, " %s=%q", name, e.attrs[name])
		}
		b.WriteString(">")
	}
	for _, c := range e.children {
		switch c := c.(type) {
		case string:
			b.WriteString(c)
		case *element:
			c.writeHTML(b)
		}
	}
	if e.tag != "" {
		b.WriteString("</" + e.tag + ">")
	}
}

// isTag matches an element with the tag name.
func isTag(name string) func(*element) bool {
	return func(e *element) bool { return e.tag == name }
}

// attrIs matches an element whose attribute name has the value.
func attrIs(name, value string) func(*element) bool {
	return func(e *element) bool { return e.has(name) && e.attrs[name] == value }
}

// hasAttr matches an element that has the attribute name, whatever its value.
func hasAttr(name string) func(*element) bool {
	return func(e *element) bool { return e.has(name) }
}

// textIs matches an element whose text is s.
func textIs(s string) func(*element) bool {
	return func(e *element) bool { return e.text() == s }
}

// text returns the text a person reads in a page or a fragment.
func text(markup string) string {
	return readHTML(markup).text()
}

// linkTo reports whether the page holds a link to href whose text is label.
func linkTo(page, href, label string) bool {
	return readHTML(page).first(isTag("a"), attrIs("href", href), textIs(label)) != nil
}
