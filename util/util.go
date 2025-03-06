package util

import (
	"log"
	"net/url"
	"fmt"
	"regexp"
	"strings"
)

func TrimUrl(u string) string {
	s := strings.TrimPrefix(u, "https://")
	return strings.TrimPrefix(s, "http://")
}

func ConstructURL(canonicalURL, path string) string {
	if !strings.HasPrefix(canonicalURL, "http") {
		canonicalURL = "https://" + canonicalURL
	}
	u, err := url.Parse(canonicalURL)
	Check(err)
	u.Path = path
	fmt.Println(u.String())
	return u.String()
}

func Check(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

// Convert markdown links to just the descriptive part (for nicer rss feed item text)
func SanitizeMarkdown(markdownIn string) string {
	removeTitlePattern := regexp.MustCompile(`^#+\s*(.*?)`)
	removeLinkPattern := regexp.MustCompile(`\[(.*?)\]\((.*?)\)`)
  patterns := []*regexp.Regexp{removeTitlePattern, removeLinkPattern}
  sanitized := markdownIn
  for _, pattern := range patterns {
    matches := pattern.FindAllSubmatch([]byte(sanitized), -1)
    for _, m := range matches {
      if len(m) >= 2 {
        original, replacement := string(m[0]), string(m[1])
        sanitized = strings.ReplaceAll(sanitized, original, replacement)
      }
    }
  }
	return sanitized
}

func Indent(s, indent string) string {
	parts := strings.Split(s, "\n")
	for i := range parts {
		parts[i] = indent + parts[i]
	}
	return strings.Join(parts, "\n")
}
