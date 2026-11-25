package inputmodel

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/heiyeluren/mz-gui/framework/go/security/leakscan"
)

var ErrInvalidClipboard = errors.New("剪贴板内容无效")

const DefaultClipboardMaxRunes = 16 * 1024

type ClipboardPayload struct {
	MediaType string
	Text      string
}

func SanitizeClipboard(payload ClipboardPayload, maxRunes int) (ClipboardPayload, error) {
	if maxRunes <= 0 {
		maxRunes = DefaultClipboardMaxRunes
	}
	mediaType := strings.TrimSpace(payload.MediaType)
	if mediaType == "" {
		mediaType = "text/plain"
	}
	if mediaType != "text/plain" && mediaType != "text/uri-list" {
		return ClipboardPayload{}, ErrInvalidClipboard
	}
	text := payload.Text
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > maxRunes {
		return ClipboardPayload{}, ErrInvalidClipboard
	}
	for _, r := range text {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return ClipboardPayload{}, ErrInvalidClipboard
		}
	}
	if findings := leakscan.Scan(text); len(findings) > 0 {
		return ClipboardPayload{}, ErrInvalidClipboard
	}
	return ClipboardPayload{MediaType: mediaType, Text: text}, nil
}
