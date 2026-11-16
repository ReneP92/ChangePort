package alerts

import "testing"

func TestValidateDocsURL(t *testing.T) {
	for _, docsURL := range []string{"https://changeport.example/docs", "https://changeport.example/docs/", "http://localhost:5174/docs"} {
		if err := ValidateDocsURL(docsURL); err != nil {
			t.Errorf("ValidateDocsURL(%q) = %v, want none", docsURL, err)
		}
	}
	for _, docsURL := range []string{
		"", "changeport.example/docs", "ftp://changeport.example/docs", "javascript:alert(1)",
		"https://changeport.example/{{ .Status }}", "https://changeport.example/docs|x", "https://changeport.example/docs>",
		"https://changeport.example/docs?a=1", "https://changeport.example/docs#top", "https://user@changeport.example/docs",
		"https://changeport.example/do cs",
	} {
		if err := ValidateDocsURL(docsURL); err == nil {
			t.Errorf("ValidateDocsURL(%q) accepted it", docsURL)
		}
	}
}
