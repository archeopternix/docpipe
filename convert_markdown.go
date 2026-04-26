package docpipe

import (
	"os"
)

func convertMarkdownFile(path string, src ImportSource) (importedDocument, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return importedDocument{}, err
	}

	meta := mdDefaultFrontmatter(src.Name, src.ModTime)
	if parsed, ok, err := mdParseFrontmatter(string(body), meta); err != nil {
		return importedDocument{}, err
	} else if ok {
		meta = parsed
	}
	meta = mdEnsureFrontmatterDefaults(meta, src.Name, src.ModTime)

	root := mdComposeMarkdownWithMeta(meta, StripFrontmatter(string(body)))
	return importedDocument{Root: []byte(root)}, nil
}
