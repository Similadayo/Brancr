package tenantapi

import (
	"archive/zip"
	"bytes"
	"io"
	"regexp"
	"strings"
)

// extractTextFromDocx does a lightweight extraction by scanning word/document.xml
// It avoids heavy external deps by reading the docx zip and extracting text nodes.
func extractTextFromDocx(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return "", err
	}
	var docXML []byte
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			fr, err := f.Open()
			if err != nil {
				return "", err
			}
			defer fr.Close()
			docXML, err = io.ReadAll(fr)
			if err != nil {
				return "", err
			}
			break
		}
	}
	if len(docXML) == 0 {
		return "", nil
	}
	// extract text inside <w:t>...</w:t>
	re := regexp.MustCompile(`<w:t[^>]*>(.*?)</w:t>`)
	matches := re.FindAllSubmatch(docXML, -1)
	var parts []string
	for _, m := range matches {
		parts = append(parts, string(m[1]))
	}
	return strings.Join(parts, " "), nil
}

// ExtractDocxFromBytes is an exported helper used by job handlers
func ExtractDocxFromBytes(b []byte) (string, error) {
	return extractTextFromDocx(bytes.NewReader(b))
}
