#!/bin/bash

# Overwrite metadata.go with the new one
cat << 'EOT' > internal/pipe/gentoo/metadata.go
package gentoo

import (
	"encoding/xml"
	"errors"

	"github.com/goreleaser/goreleaser/v2/pkg/config"
)

type gentooInnerNode struct {
	XMLName xml.Name
	Content string            `xml:",chardata"`
	Attrs   []xml.Attr        `xml:",any,attr"`
	Nodes   []gentooInnerNode `xml:",any"`
}

type gentooMaintainer struct {
	Type  string `xml:"type,attr,omitempty"`
	Email string `xml:"email"`
	Name  string `xml:"name,omitempty"`
}

type gentooUpstream struct {
	BugsTo string            `xml:"bugs-to,omitempty"`
	Doc    string            `xml:"doc,omitempty"`
	Attrs  []xml.Attr        `xml:",any,attr"`
	Nodes  []gentooInnerNode `xml:",any"`
}

type gentooUseFlag struct {
	XMLName xml.Name   `xml:"flag"`
	Name    string     `xml:"name,attr"`
	Value   string     `xml:",chardata"`
	Attrs   []xml.Attr `xml:",any,attr"`
}

type gentooUse struct {
	XMLName xml.Name          `xml:"use"`
	Flags   []gentooUseFlag   `xml:"flag"`
	Attrs   []xml.Attr        `xml:",any,attr"`
	Nodes   []gentooInnerNode `xml:",any"`
}

type Metadata struct {
	XMLName     xml.Name           `xml:"pkgmetadata"`
	Attrs       []xml.Attr         `xml:",any,attr"`
	Maintainers []gentooMaintainer `xml:"maintainer"`
	Use         *gentooUse         `xml:"use,omitempty"`
	Upstream    *gentooUpstream    `xml:"upstream,omitempty"`
	InnerNodes  []gentooInnerNode  `xml:",any"`
}

func NewMetadata() *Metadata {
	return &Metadata{}
}

func ParseMetadata(content []byte) (*Metadata, error) {
	m := &Metadata{}
	if err := xml.Unmarshal(content, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Metadata) AddMaintainers(maintainers []config.GentooMaintainer) error {
	for _, main := range maintainers {
		if main.Email == "" {
			return errors.New("maintainer email is required")
		}
		exists := false
		for _, em := range m.Maintainers {
			if em.Email == main.Email {
				exists = true
				break
			}
		}
		if !exists {
			m.Maintainers = append(m.Maintainers, gentooMaintainer{
				Type:  "person",
				Email: main.Email,
				Name:  main.Name,
			})
		}
	}
	return nil
}

func (m *Metadata) AddUseFlags(flags []config.GentooUseFlag) {
	if len(flags) == 0 {
		return
	}
	if m.Use == nil {
		m.Use = &gentooUse{}
	}
	configuredFlags := make(map[string]string)
	for _, f := range flags {
		configuredFlags[f.Flag] = f.Description
	}
	for i, f := range m.Use.Flags {
		if v, ok := configuredFlags[f.Name]; ok {
			m.Use.Flags[i].Value = v
			delete(configuredFlags, f.Name)
		}
	}
	for _, f := range flags {
		if v, ok := configuredFlags[f.Flag]; ok {
			m.Use.Flags = append(m.Use.Flags, gentooUseFlag{
				Name:  f.Flag,
				Value: v,
			})
		}
	}
}

func (m *Metadata) SetUpstream(bugsTo string) {
	if bugsTo == "" {
		return
	}
	if m.Upstream == nil {
		m.Upstream = &gentooUpstream{}
	}
	m.Upstream.BugsTo = bugsTo
}

func (m *Metadata) Render() ([]byte, error) {
	content, err := xml.MarshalIndent(m, "", "\t")
	if err != nil {
		return nil, err
	}
	header := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE pkgmetadata SYSTEM \"https://www.gentoo.org/dtd/metadata.dtd\">\n")
	return append(header, append(content, '\n')...), nil
}
EOT

# Restore test files to correct version since `git reset --hard` wiped them
cat << 'EOT' >> internal/pipe/gentoo/utils.go

import "bytes"

func stripComments(content []byte) []byte {
	var result []byte
	for line := range bytes.SplitSeq(content, []byte{'\n'}) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] != '#' {
			result = append(result, line...)
			result = append(result, '\n')
		}
	}
	return result
}
EOT
