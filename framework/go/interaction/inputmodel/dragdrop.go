package inputmodel

import (
	"errors"
	"net/url"
	"path"
	"strings"
)

var ErrInvalidDragDrop = errors.New("拖放内容无效")

const DefaultDragDropMaxRunes = 16 * 1024

type DragDropOrigin string

const (
	OriginInternalWindow DragDropOrigin = "internal-window"
	OriginExternal       DragDropOrigin = "external"
	OriginUnknown        DragDropOrigin = "unknown"
)

type DragDropPayload struct {
	Meta      RequestMeta
	MediaType string
	Text      string
	Origin    DragDropOrigin
}

func (origin DragDropOrigin) Valid() bool {
	switch origin {
	case OriginInternalWindow, OriginExternal, OriginUnknown:
		return true
	default:
		return false
	}
}

func SanitizeDragDrop(payload DragDropPayload, maxRunes int) (DragDropPayload, error) {
	if err := payload.Meta.Validate(); err != nil || !payload.Origin.Valid() {
		return DragDropPayload{}, ErrInvalidDragDrop
	}
	if maxRunes <= 0 {
		maxRunes = DefaultDragDropMaxRunes
	}
	cleaned, err := SanitizeClipboard(ClipboardPayload{MediaType: payload.MediaType, Text: payload.Text}, maxRunes)
	if err != nil {
		return DragDropPayload{}, ErrInvalidDragDrop
	}
	if cleaned.MediaType == "text/uri-list" {
		if err := checkURIList(cleaned.Text); err != nil {
			return DragDropPayload{}, err
		}
	}
	return DragDropPayload{
		Meta:      payload.Meta,
		MediaType: cleaned.MediaType,
		Text:      cleaned.Text,
		Origin:    payload.Origin,
	}, nil
}

func checkURIList(text string) error {
	found := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parsed, err := url.Parse(line)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" && parsed.Scheme != "file" {
			return ErrInvalidDragDrop
		}
		scheme := strings.ToLower(parsed.Scheme)
		if scheme == "javascript" || scheme == "data" || scheme == "vbscript" || scheme == "http" {
			return ErrInvalidDragDrop
		}
		if scheme != "https" && scheme != "file" {
			return ErrInvalidDragDrop
		}
		if scheme == "file" && isExecutableURIPath(parsed.Path) {
			return ErrInvalidDragDrop
		}
		found = true
	}
	if !found {
		return ErrInvalidDragDrop
	}
	return nil
}

func isExecutableURIPath(rawPath string) bool {
	ext := strings.ToLower(path.Ext(rawPath))
	switch ext {
	case ".exe", ".bat", ".cmd", ".com", ".scr", ".ps1", ".sh", ".msi", ".dmg", ".pkg", ".command":
		return true
	default:
		return false
	}
}
