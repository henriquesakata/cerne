package app

import (
	"net"
	"testing"
)

// F-0503 crit. 4: the local-use NAT64 prefix 64:ff9b:1::/48 is judged by the
// IPv4 address it embeds (RFC 6052: bytes 6, 7, 9 and 10), and Teredo 2001::/32
// is not public.
func TestPublicIPLocalNAT64AndTeredo(t *testing.T) {
	for ip, want := range map[string]bool{
		"64:ff9b:1:a00:0:100::":                false, // 10.0.0.1
		"64:ff9b:1:7f00:0:100::":               false, // 127.0.0.1
		"64:ff9b:1:c0a8:0:100::":               false, // 192.168.0.1
		"64:ff9b:1:5db8:d7:e00::":              true,  // 93.184.215.14
		"2001:0:4136:e378:8000:63bf:3fff:fdd2": false, // Teredo
		"2001::1":                              false,
		"2001:4860:4860::8888":                 true, // an ordinary 2001: address
	} {
		if got := publicIP(net.ParseIP(ip)); got != want {
			t.Errorf("publicIP(%s) = %v, want %v", ip, got, want)
		}
	}
}

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
