package contract

type MediaSource struct {
	Meta RequestMeta `json:"meta"`
	URI  string      `json:"uri"`
}

type MediaInfo struct {
	MIMEType string `json:"mime_type"`
	Bytes    int64  `json:"bytes"`
	Duration int64  `json:"duration_ms,omitempty"`
}

type ThumbnailRequest struct {
	Meta   RequestMeta `json:"meta"`
	Source MediaSource `json:"source"`
	Width  int         `json:"width"`
	Height int         `json:"height"`
}

type PlaybackRequest struct {
	Meta   RequestMeta `json:"meta"`
	Source MediaSource `json:"source"`
}

type PlaybackSession struct {
	ID PlaybackSessionID `json:"id"`
}

type PlaybackCommand struct {
	Meta      RequestMeta       `json:"meta"`
	SessionID PlaybackSessionID `json:"session_id"`
	Kind      string            `json:"kind"`
}

type PlaybackState struct {
	SessionID PlaybackSessionID `json:"session_id"`
	State     string            `json:"state"`
	Position  int64             `json:"position_ms"`
}
