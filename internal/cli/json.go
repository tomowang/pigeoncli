package cli

import (
	"encoding/json"
	"io"
	"time"

	"github.com/tomowang/pigeoncli/internal/core/folder"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

// writeJSON marshals v as indented JSON to w. json.Encoder.Encode appends a
// trailing newline, which is what makes piping several commands' output
// into `jq` (or just eyeballing it in a terminal) behave the way a person
// expects.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// jsonMessage is message.Message's --json representation: explicit,
// stable field names and tags kept in internal/cli rather than added to
// the core DTO itself, so internal/core stays unaware that JSON is one of
// its consumers' output formats. Unread/Starred are included pre-computed
// (Message.IsRead/IsStarred) since Flags' raw IMAP strings aren't a format
// a script should have to know how to interpret.
type jsonMessage struct {
	UID        uint32    `json:"uid"`
	MessageID  string    `json:"message_id,omitempty"`
	InReplyTo  string    `json:"in_reply_to,omitempty"`
	References []string  `json:"references,omitempty"`
	Subject    string    `json:"subject"`
	FromName   string    `json:"from_name,omitempty"`
	FromAddr   string    `json:"from_addr"`
	ToAddrs    []string  `json:"to_addrs,omitempty"`
	CcAddrs    []string  `json:"cc_addrs,omitempty"`
	Date       time.Time `json:"date"`
	Flags      []string  `json:"flags,omitempty"`
	Size       int64     `json:"size"`
	Unread     bool      `json:"unread"`
	Starred    bool      `json:"starred"`
}

func toJSONMessage(m message.Message) jsonMessage {
	return jsonMessage{
		UID: m.UID, MessageID: m.MessageID, InReplyTo: m.InReplyTo, References: m.References,
		Subject: m.Subject, FromName: m.FromName, FromAddr: m.FromAddr,
		ToAddrs: m.ToAddrs, CcAddrs: m.CcAddrs, Date: m.Date, Flags: m.Flags, Size: m.Size,
		Unread: !m.IsRead(), Starred: m.IsStarred(),
	}
}

func toJSONMessages(msgs []message.Message) []jsonMessage {
	out := make([]jsonMessage, len(msgs))
	for i, m := range msgs {
		out[i] = toJSONMessage(m)
	}
	return out
}

// jsonSearchResult is message.SearchResult's --json representation.
type jsonSearchResult struct {
	jsonMessage
	Folder string `json:"folder"`
}

func toJSONSearchResults(results []message.SearchResult) []jsonSearchResult {
	out := make([]jsonSearchResult, len(results))
	for i, r := range results {
		out[i] = jsonSearchResult{jsonMessage: toJSONMessage(r.Message), Folder: r.FolderPath}
	}
	return out
}

// jsonAttachment is message.Attachment's --json representation.
type jsonAttachment struct {
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

func toJSONAttachments(atts []message.Attachment) []jsonAttachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]jsonAttachment, len(atts))
	for i, a := range atts {
		out[i] = jsonAttachment{Index: a.Index, Filename: a.Filename, ContentType: a.ContentType, Size: a.Size}
	}
	return out
}

// jsonShownMessage is `message show`'s --json representation: a header
// plus the rendered body and attachment list. It omits Body.Raw — the
// undecoded source isn't a JSON-friendly shape, and `message show` never
// exposed it in its text output either.
type jsonShownMessage struct {
	jsonMessage
	Body        string           `json:"body"`
	Attachments []jsonAttachment `json:"attachments,omitempty"`
}

// jsonFolder is folder.Folder's --json representation.
type jsonFolder struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	SpecialUse  string `json:"special_use,omitempty"`
	TotalCount  int    `json:"total_count"`
	UnreadCount int    `json:"unread_count"`
}

func toJSONFolders(folders []folder.Folder) []jsonFolder {
	out := make([]jsonFolder, len(folders))
	for i, f := range folders {
		out[i] = jsonFolder{Path: f.Path, Name: f.Name, SpecialUse: f.SpecialUse, TotalCount: f.TotalCount, UnreadCount: f.UnreadCount}
	}
	return out
}
