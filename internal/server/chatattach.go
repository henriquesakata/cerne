package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/henriquesakata/cerne/internal/project"
)

// Images attached to a Backlog chat message (F-0520) are input for the agent,
// not documentation: they live in the project cache, per conversation, and
// never reach the backend, .docs/ or the repository tree.
const (
	maxChatImageBytes       = 10 << 20
	maxChatImagesPerMessage = 5
	// chatAttachmentTTL is how long a conversation's attachments outlive their
	// last upload; the sweep runs on every upload.
	chatAttachmentTTL = 30 * 24 * time.Hour
)

// chatImageExt is the closed set of accepted images, keyed by the type
// http.DetectContentType reads from the bytes — never from the header or a
// file name the browser sent.
var chatImageExt = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/webp": "webp",
	"image/gif":  "gif",
}

// attachmentIDPattern is the only shape of an attachment id: the name the
// server generated. Anything else never becomes a path.
var attachmentIDPattern = regexp.MustCompile(`^[0-9a-f]{16}\.(png|jpg|webp|gif)$`)

var errBadAttachmentRef = errors.New("invalid conversation or attachment id")

func chatAttachmentRoot(projectID string) (string, error) {
	cache, err := project.CacheDir(projectID)
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "chat-attachments"), nil
}

// chatAttachmentDir is the directory of one conversation's attachments.
func chatAttachmentDir(projectID, conversationID string) (string, error) {
	if !conversationIDPattern.MatchString(conversationID) {
		return "", errBadAttachmentRef
	}
	root, err := chatAttachmentRoot(projectID)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, conversationID), nil
}

// chatAttachmentPath is the file of one attachment; both ids are validated.
func chatAttachmentPath(projectID, conversationID, id string) (string, error) {
	if !attachmentIDPattern.MatchString(id) {
		return "", errBadAttachmentRef
	}
	dir, err := chatAttachmentDir(projectID, conversationID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id), nil
}

func writeAttachmentRefError(w http.ResponseWriter, err error) {
	if errors.Is(err, errBadAttachmentRef) {
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
}

// handleChatAttachmentUpload stores the request body, one image, under
// ?conversation_id= and answers its id.
func handleChatAttachmentUpload(w http.ResponseWriter, r *http.Request, projectID string) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin request refused"))
		return
	}
	dir, err := chatAttachmentDir(projectID, r.URL.Query().Get("conversation_id"))
	if err != nil {
		writeAttachmentRefError(w, err)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxChatImageBytes))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody("image larger than 10 MB"))
		return
	}
	mediaType := http.DetectContentType(data)
	ext, ok := chatImageExt[mediaType]
	if !ok {
		writeJSON(w, http.StatusUnsupportedMediaType, errorBody("not a PNG, JPEG, WebP or GIF image"))
		return
	}
	sweepChatAttachments(filepath.Dir(dir), time.Now())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	id := hex.EncodeToString(raw[:]) + "." + ext
	if err := os.WriteFile(filepath.Join(dir, id), data, 0o600); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "media_type": mediaType})
}

// handleChatAttachmentGet serves one stored image back to the chat's bubble.
func handleChatAttachmentGet(w http.ResponseWriter, projectID, conversationID, id string) {
	path, err := chatAttachmentPath(projectID, conversationID, id)
	if err != nil {
		writeAttachmentRefError(w, err)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("attachment expired"))
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}

// handleChatAttachmentsDelete removes a conversation's attachments.
func handleChatAttachmentsDelete(w http.ResponseWriter, r *http.Request, projectID, conversationID string) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin request refused"))
		return
	}
	dir, err := chatAttachmentDir(projectID, conversationID)
	if err != nil {
		writeAttachmentRefError(w, err)
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sweepChatAttachments removes the conversation directories under root whose
// last upload is older than chatAttachmentTTL. Best effort: a failure here
// never costs the upload that triggered it.
func sweepChatAttachments(root string, now time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !e.IsDir() || !conversationIDPattern.MatchString(e.Name()) {
			continue
		}
		if now.Sub(info.ModTime()) > chatAttachmentTTL {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}
