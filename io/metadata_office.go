package io

import (
	"archive/zip"
	"encoding/xml"
	stdio "io"
	"strings"
)

type officeCoreProperties struct {
	XMLName     xml.Name `xml:"coreProperties"`
	Title       string   `xml:"title"`
	Subject     string   `xml:"subject"`
	Creator     string   `xml:"creator"`
	Keywords    string   `xml:"keywords"`
	Description string   `xml:"description"`
	Language    string   `xml:"language"`
	Revision    string   `xml:"revision"`
}

func readOfficeMetaData(path string, meta *MetaData) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()

	props, err := readOfficeCorePropertiesFromFiles(reader.File)
	if err != nil {
		return err
	}
	applyOfficeCoreProperties(meta, props)
	return nil
}

func readOfficeCorePropertiesFromFiles(files []*zip.File) (officeCoreProperties, error) {
	for _, file := range files {
		if file.Name != "docProps/core.xml" {
			continue
		}
		return readOfficeCoreProperties(file)
	}
	return officeCoreProperties{}, nil
}

func readOfficeCoreProperties(file *zip.File) (officeCoreProperties, error) {
	var props officeCoreProperties
	rc, err := file.Open()
	if err != nil {
		return props, err
	}
	defer rc.Close()

	decoder := xml.NewDecoder(rc)
	if err := decoder.Decode(&props); err != nil && err != stdio.EOF {
		return props, err
	}
	return props, nil
}

func applyOfficeCoreProperties(meta *MetaData, props officeCoreProperties) {
	if meta == nil {
		return
	}
	meta.Author = strings.TrimSpace(props.Creator)
	meta.Title = strings.TrimSpace(props.Title)
	if meta.Version == "" {
		meta.Version = normalizeVersion(props.Revision)
	}
	if meta.Language == "" {
		meta.Language = normalizeLanguageCode(props.Language)
	}

	description := strings.TrimSpace(props.Description)
	if description != "" {
		meta.Abstract = description
	} else {
		meta.Abstract = strings.TrimSpace(props.Subject)
	}
	meta.Keywords = normalizeKeywords(props.Keywords)
}
