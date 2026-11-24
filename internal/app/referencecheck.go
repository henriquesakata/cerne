package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"strings"

	"golang.org/x/net/html"
)

// referenceTextHash returns the SHA-256 (hex) of what a reader of the page sees
// (F-0501): for HTML, the text outside script/style/noscript/template with
// whitespace collapsed; for any other content type, the raw body.
func referenceTextHash(contentType string, body []byte) string {
	mt, _, _ := mime.ParseMediaType(contentType)
	data := body
	if mt == "text/html" || mt == "application/xhtml+xml" {
		data = []byte(htmlVisibleText(body))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func htmlVisibleText(body []byte) string {
	var sb strings.Builder
	z := html.NewTokenizer(bytes.NewReader(body))
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.Join(strings.Fields(sb.String()), " ")
		case html.StartTagToken:
			if skippedTag(z) {
				skip++
			}
		case html.EndTagToken:
			if skippedTag(z) && skip > 0 {
				skip--
			}
		case html.TextToken:
			if skip == 0 {
				sb.Write(z.Text())
				sb.WriteByte(' ')
			}
		}
	}
}

func skippedTag(z *html.Tokenizer) bool {
	name, _ := z.TagName()
	switch string(name) {
	case "script", "style", "noscript", "template":
		return true
	}
	return false
}
