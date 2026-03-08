package music

import (
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

var directAudioExtensions = map[string]struct{}{
	".aac":  {},
	".flac": {},
	".m4a":  {},
	".mp3":  {},
	".oga":  {},
	".ogg":  {},
	".opus": {},
	".wav":  {},
	".weba": {},
	".webm": {},
}

func isDirectAudioURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	return hasDirectAudioExtension(path.Ext(u.Path))
}

func looksLikeDirectFromHeaders(u *url.URL, header http.Header) bool {
	if looksLikeAudioContentType(header.Get("Content-Type")) {
		return true
	}
	if filename := filenameFromContentDisposition(header.Get("Content-Disposition")); hasDirectAudioExtension(path.Ext(filename)) {
		return true
	}
	return isDirectAudioURL(u)
}

func looksLikeAudioContentType(value string) bool {
	if value == "" {
		return false
	}

	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		mediaType = strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
	}
	mediaType = strings.ToLower(mediaType)
	return strings.HasPrefix(mediaType, "audio/") || mediaType == "application/ogg" || mediaType == "video/ogg"
}

func resolveDirectTitle(rawURL string, resolvedURL *url.URL, header http.Header) string {
	if title := filenameFromContentDisposition(header.Get("Content-Disposition")); title != "" {
		return title
	}
	if title := basenameFromURL(resolvedURL); title != "" {
		return title
	}
	if parsedURL, err := url.Parse(rawURL); err == nil {
		if title := basenameFromURL(parsedURL); title != "" {
			return title
		}
	}
	return rawURL
}

func basenameFromURL(u *url.URL) string {
	if u == nil {
		return ""
	}

	base := path.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	if decoded, err := url.PathUnescape(base); err == nil && decoded != "" {
		base = decoded
	}
	return base
}

func filenameFromContentDisposition(value string) string {
	if value == "" {
		return ""
	}

	_, params, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}

	if filename := decodeRFC5987(params["filename*"]); filename != "" {
		return strings.TrimSpace(filename)
	}
	return strings.TrimSpace(params["filename"])
}

func decodeRFC5987(value string) string {
	if value == "" {
		return ""
	}

	parts := strings.SplitN(value, "''", 2)
	if len(parts) == 2 {
		if decoded, err := url.PathUnescape(parts[1]); err == nil && decoded != "" {
			return decoded
		}
		return parts[1]
	}
	if decoded, err := url.PathUnescape(value); err == nil && decoded != "" {
		return decoded
	}
	return value
}

func hasDirectAudioExtension(ext string) bool {
	_, ok := directAudioExtensions[strings.ToLower(ext)]
	return ok
}

func directTempFileSuffix(title string, resolvedURL *url.URL) string {
	ext := path.Ext(title)
	if !hasDirectAudioExtension(ext) && resolvedURL != nil {
		ext = path.Ext(resolvedURL.Path)
	}
	if !hasDirectAudioExtension(ext) {
		return ""
	}
	return strings.ToLower(ext)
}
