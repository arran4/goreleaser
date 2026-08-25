package gentoo

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
)

type metadataNodeKind uint8

const (
	metadataElementNode metadataNodeKind = iota
	metadataTextNode
	metadataCommentNode
)

type metadataNode struct {
	kind     metadataNodeKind
	name     xml.Name
	attrs    []xml.Attr
	text     string
	children []*metadataNode
}

// Metadata is an ordered XML tree. Managed maintainer, USE, and upstream
// fields are updated in place, while unknown elements, attributes, child
// content, comments, and sibling ordering remain intact.
type Metadata struct {
	root *metadataNode
}

func NewMetadata() *Metadata {
	return &Metadata{root: &metadataNode{kind: metadataElementNode, name: xml.Name{Local: "pkgmetadata"}}}
}

func ParseMetadata(content []byte) (*Metadata, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("metadata.xml has no root element")
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "pkgmetadata" {
			return nil, fmt.Errorf("metadata.xml root must be pkgmetadata, got %s", start.Name.Local)
		}
		root, err := parseMetadataElement(decoder, start)
		if err != nil {
			return nil, err
		}
		return &Metadata{root: root}, nil
	}
}

func parseMetadataElement(decoder *xml.Decoder, start xml.StartElement) (*metadataNode, error) {
	node := &metadataNode{kind: metadataElementNode, name: start.Name, attrs: slices.Clone(start.Attr)}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			child, err := parseMetadataElement(decoder, value)
			if err != nil {
				return nil, err
			}
			node.children = append(node.children, child)
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				node.children = append(node.children, &metadataNode{kind: metadataTextNode, text: string(value)})
			}
		case xml.Comment:
			node.children = append(node.children, &metadataNode{kind: metadataCommentNode, text: string(value)})
		case xml.EndElement:
			return node, nil
		}
	}
}

func (m *Metadata) AddMaintainers(maintainers []config.GentooMaintainer) error {
	m.ensureRoot()
	for _, maintainer := range maintainers {
		if maintainer.Email == "" {
			return errors.New("maintainer email is required")
		}
		typ := maintainer.Type
		if typ == "" {
			typ = "person"
		} else if typ != "person" && typ != "project" {
			return fmt.Errorf("invalid gentoo maintainer type %q: must be person or project", typ)
		}

		node := m.maintainer(maintainer.Email)
		if node == nil {
			node = newMetadataElement("maintainer", xml.Attr{Name: xml.Name{Local: "type"}, Value: typ})
			node.children = append(node.children, metadataTextElement("email", maintainer.Email))
			m.root.children = append(m.root.children, node)
		} else {
			node.setAttr("type", typ)
		}
		if maintainer.Name != "" {
			node.setChildText("name", maintainer.Name)
		}
	}
	return nil
}

func (m *Metadata) maintainer(email string) *metadataNode {
	m.ensureRoot()
	for _, node := range m.root.elements("maintainer") {
		if node.childText("email") == email {
			return node
		}
	}
	return nil
}

func (m *Metadata) MaintainerEmails() []string {
	m.ensureRoot()
	var result []string
	for _, node := range m.root.elements("maintainer") {
		if email := node.childText("email"); email != "" {
			result = append(result, email)
		}
	}
	return result
}

func (m *Metadata) AddUseFlags(flags []config.GentooUseFlag) {
	m.ensureRoot()
	if len(flags) == 0 {
		return
	}
	use := m.root.firstElement("use")
	if use == nil {
		use = newMetadataElement("use")
		m.root.children = append(m.root.children, use)
	}
	configured := map[string]string{}
	for _, flag := range flags {
		if flag.Description != "" {
			configured[strings.TrimLeft(flag.Flag, "+-")] = flag.Description
		}
	}
	names := make([]string, 0, len(configured))
	for name := range configured {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		flag := use.elementByAttr("flag", "name", name)
		if flag == nil {
			flag = newMetadataElement("flag", xml.Attr{Name: xml.Name{Local: "name"}, Value: name})
			use.children = append(use.children, flag)
		}
		flag.setText(configured[name])
	}
}

func (m *Metadata) SetLongDescription(desc string) {
	m.ensureRoot()
	if desc == "" {
		return
	}
	for _, child := range m.root.elements("longdescription") {
		if len(child.attrs) == 0 {
			child.setText(desc)
			return
		}
	}
	node := metadataTextElement("longdescription", desc)
	m.root.children = append(m.root.children, node)
}

func (m *Metadata) SetUpstream(bugsTo, doc string, remoteIDs []config.GentooUpstreamRemoteID) {
	m.ensureRoot()
	if bugsTo == "" && doc == "" && len(remoteIDs) == 0 {
		return
	}
	upstream := m.root.firstElement("upstream")
	if upstream == nil {
		upstream = newMetadataElement("upstream")
		m.root.children = append(m.root.children, upstream)
	}
	if bugsTo != "" {
		upstream.setChildText("bugs-to", bugsTo)
	}
	if doc != "" {
		found := false
		for _, child := range upstream.elements("doc") {
			if len(child.attrs) == 0 {
				child.setText(doc)
				found = true
				break
			}
		}
		if !found {
			child := metadataTextElement("doc", doc)
			upstream.children = append(upstream.children, child)
		}
	}
	for _, rid := range remoteIDs {
		if rid.Type == "" || rid.ID == "" {
			continue
		}

		found := false
		for _, child := range upstream.elements("remote-id") {
			for _, attr := range child.attrs {
				if attr.Name.Local == "type" && attr.Value == rid.Type && child.textContent() == rid.ID {
					found = true
					break
				}
			}
		}

		if !found {
			child := metadataTextElement("remote-id", rid.ID)
			child.setAttr("type", rid.Type)
			upstream.children = append(upstream.children, child)
		}
	}
}

func (m *Metadata) Render() ([]byte, error) {
	if m == nil || m.root == nil {
		m = NewMetadata()
	}
	var body bytes.Buffer
	encoder := xml.NewEncoder(&body)
	encoder.Indent("", "\t")
	if err := encodeMetadataNode(encoder, m.root); err != nil {
		return nil, err
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	header := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE pkgmetadata SYSTEM \"https://www.gentoo.org/dtd/metadata.dtd\">\n")
	return append(header, append(body.Bytes(), '\n')...), nil
}

func (m *Metadata) Marshal() ([]byte, error) { return m.Render() }

func (m *Metadata) ensureRoot() {
	if m.root == nil {
		m.root = NewMetadata().root
	}
}

func encodeMetadataNode(encoder *xml.Encoder, node *metadataNode) error {
	switch node.kind {
	case metadataTextNode:
		return encoder.EncodeToken(xml.CharData(node.text))
	case metadataCommentNode:
		return encoder.EncodeToken(xml.Comment(node.text))
	}
	start := xml.StartElement{Name: node.name, Attr: slices.Clone(node.attrs)}
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	for _, child := range node.children {
		if err := encodeMetadataNode(encoder, child); err != nil {
			return err
		}
	}
	return encoder.EncodeToken(start.End())
}

func newMetadataElement(name string, attrs ...xml.Attr) *metadataNode {
	return &metadataNode{kind: metadataElementNode, name: xml.Name{Local: name}, attrs: slices.Clone(attrs)}
}

func metadataTextElement(name, value string) *metadataNode {
	node := newMetadataElement(name)
	node.setText(value)
	return node
}

func (n *metadataNode) setAttr(name, value string) {
	for i, attr := range n.attrs {
		if attr.Name.Local == name {
			n.attrs[i].Value = value
			return
		}
	}
	n.attrs = append(n.attrs, xml.Attr{Name: xml.Name{Local: name}, Value: value})
}

func (n *metadataNode) elements(name string) []*metadataNode {
	var result []*metadataNode
	for _, child := range n.children {
		if child.kind == metadataElementNode && child.name.Local == name {
			result = append(result, child)
		}
	}
	return result
}

func (n *metadataNode) firstElement(name string) *metadataNode {
	elements := n.elements(name)
	if len(elements) == 0 {
		return nil
	}
	return elements[0]
}

func (n *metadataNode) elementByAttr(element, attr, value string) *metadataNode {
	for _, child := range n.elements(element) {
		for _, candidate := range child.attrs {
			if candidate.Name.Local == attr && candidate.Value == value {
				return child
			}
		}
	}
	return nil
}

func (n *metadataNode) childText(name string) string {
	child := n.firstElement(name)
	if child == nil {
		return ""
	}
	return child.textContent()
}

func (n *metadataNode) textContent() string {
	var result strings.Builder
	for _, child := range n.children {
		if child.kind == metadataTextNode {
			result.WriteString(child.text)
		}
	}
	return strings.TrimSpace(result.String())
}

func (n *metadataNode) setChildText(name, value string) {
	child := n.firstElement(name)
	if child == nil {
		child = newMetadataElement(name)
		n.children = append(n.children, child)
	}
	child.setText(value)
}

func (n *metadataNode) setText(value string) {
	result := n.children[:0]
	inserted := false
	for _, child := range n.children {
		if child.kind != metadataTextNode {
			result = append(result, child)
			continue
		}
		if !inserted {
			result = append(result, &metadataNode{kind: metadataTextNode, text: value})
			inserted = true
		}
	}
	if !inserted {
		result = append([]*metadataNode{{kind: metadataTextNode, text: value}}, result...)
	}
	n.children = result
}

// Layout is the effective overlay layout.conf policy after explicit config
// overrides are applied.
type Layout struct {
	hashes                    []string
	thin                      bool
	cacheFormats              []string
	hasCacheFormatsConfigured bool
}

func (l Layout) ManifestHashes() []string { return slices.Clone(l.hashes) }
func (l Layout) ThinManifests() bool      { return l.thin }
func (l Layout) SupportsMetaCache() bool {
	return !l.hasCacheFormatsConfigured || slices.Contains(l.cacheFormats, "md5-dict") || slices.Contains(l.cacheFormats, "md5-cache")
}

func ParseLayout(content []byte) Layout {
	settings := Layout{hashes: []string{"BLAKE2B", "SHA512"}}
	for lineB := range bytes.SplitSeq(content, []byte{'\n'}) {
		key, value, ok := strings.Cut(strings.TrimSpace(string(lineB)), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "manifest-hashes":
			settings.hashes = strings.Fields(value)
		case "thin-manifests":
			settings.thin = strings.TrimSpace(value) == "true"
		case "cache-formats":
			settings.hasCacheFormatsConfigured = true
			settings.cacheFormats = strings.Fields(value)
		}
	}
	return settings
}

func (l Layout) WithConfig(cfg manifestConfig) Layout {
	if len(cfg.hashes) > 0 {
		l.hashes = slices.Clone(cfg.hashes)
	}
	if cfg.thin != nil {
		l.thin = *cfg.thin
	}
	return l
}

func prepareMetadata(state *Metadata, cfg metadataConfig, changes *ChangeSet, path string) error {
	if cfg.Empty() {
		return nil
	}
	state.AddUseFlags(cfg.useFlags)
	if err := state.AddMaintainers(cfg.maintainers); err != nil {
		return err
	}
	state.SetLongDescription(cfg.longDescription)
	state.SetUpstream(cfg.upstream.BugsTo, cfg.upstream.Doc, cfg.upstream.RemoteIDs)
	content, err := state.Render()
	if err != nil {
		return err
	}
	changes.Write(path, content)
	return nil
}
