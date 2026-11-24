package app

import (
	"testing"
)

// F-0503 crit. 6: the chat-share hosts match as the exact host or with www.,
// any other subdomain (learn.chatgpt.com) passes; the answer-engine and
// short-link hosts match with subdomains; gemini.google.com only exactly.
func TestShareLinkChatHostsRound4(t *testing.T) {
	for host, want := range map[string]bool{
		"chatgpt.com": true, "www.chatgpt.com": true, "chat.openai.com": true, "www.chat.openai.com": true,
		"claude.ai": true, "www.claude.ai": true, "learn.chatgpt.com": false, "docs.claude.ai": false,
		"perplexity.ai": true, "www.perplexity.ai": true, "x.perplexity.ai": true,
		"claude.site": true, "abc.claude.site": true, "g.co": true, "x.g.co": true,
		"gemini.google.com": true, "ai.gemini.google.com": false, "docs.google.com.evil.org": false, "google.com": false,
	} {
		if got := shareLinkHost(host); got != want {
			t.Errorf("shareLinkHost(%s) = %v, want %v", host, got, want)
		}
	}
}
