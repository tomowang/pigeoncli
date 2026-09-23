package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core"
	"github.com/tomowang/pigeoncli/internal/core/compose"
	"github.com/tomowang/pigeoncli/internal/core/message"
)

func newComposeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Compose and send mail",
	}
	cmd.AddCommand(newComposeSendCmd())
	cmd.AddCommand(newComposeReplyCmd())
	cmd.AddCommand(newComposeForwardCmd())
	cmd.AddCommand(newComposeDraftsCmd())
	cmd.AddCommand(newComposeSendDraftCmd())
	cmd.AddCommand(newComposeDiscardDraftCmd())
	return cmd
}

// draftFlags is the --draft/--replace/--drafts-folder flag set shared by
// send, reply, and forward: save to the Drafts folder instead of sending.
type draftFlags struct {
	asDraft     bool
	replaceUID  uint32
	draftFolder string
}

func (f *draftFlags) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.asDraft, "draft", false, "save to the Drafts folder instead of sending")
	cmd.Flags().Uint32Var(&f.replaceUID, "replace", 0, "with --draft, replace this existing draft's UID instead of saving a new one")
	cmd.Flags().StringVar(&f.draftFolder, "drafts-folder", "", "override the auto-detected Drafts folder path (rarely needed)")
}

// finish either saves draft to the Drafts folder (f.asDraft) or sends it,
// printing an appropriate confirmation either way. sentText is the
// success message for the send path, e.g. "Message sent to %s.".
func (f *draftFlags) finish(cmd *cobra.Command, composeSvc *compose.Service, cfg config.Account, draft compose.Draft, sentText string) error {
	if f.replaceUID != 0 && !f.asDraft {
		return fmt.Errorf("--replace requires --draft")
	}
	if !f.asDraft {
		if err := composeSvc.Send(cmd.Context(), cfg, draft); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), sentText)
		return nil
	}

	prev := compose.DraftRef{}
	if f.replaceUID != 0 {
		path, err := f.resolveDraftsFolder(cmd, composeSvc, cfg.Slug)
		if err != nil {
			return err
		}
		prev = compose.DraftRef{FolderPath: path, UID: f.replaceUID}
	}
	ref, err := composeSvc.SaveDraft(cmd.Context(), cfg, draft, prev)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Draft saved (uid %d in %q).\n", ref.UID, ref.FolderPath)
	return nil
}

func (f *draftFlags) resolveDraftsFolder(cmd *cobra.Command, composeSvc *compose.Service, slug string) (string, error) {
	if f.draftFolder != "" {
		return f.draftFolder, nil
	}
	return composeSvc.DraftsFolder(cmd.Context(), slug)
}

func newComposeSendCmd() *cobra.Command {
	var to, cc, bcc, attach []string
	var subject, body string
	var df draftFlags
	cmd := &cobra.Command{
		Use:   "send <slug>",
		Short: "Compose and send a new message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			if len(to) == 0 && !df.asDraft {
				return fmt.Errorf("at least one --to recipient is required (or pass --draft to save one without recipients yet)")
			}
			attachments, err := loadAttachments(attach)
			if err != nil {
				return err
			}

			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			cfg, err := acctSvc.Get(cmd.Context(), slug)
			if err != nil {
				return err
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			bodyText := body
			if bodyText == "" {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read body from stdin: %w", err)
				}
				bodyText = string(b)
			}

			composeSvc := compose.NewService(st.Folder, st.Signature)
			draft := composeSvc.NewMessage(cmd.Context(), cfg)
			draft.To = to
			draft.Cc = cc
			draft.Bcc = bcc
			draft.Subject = subject
			draft.Body = joinBody(bodyText, draft.Body)
			draft.Attachments = attachments

			return df.finish(cmd, composeSvc, cfg, draft, fmt.Sprintf("Message sent to %s.", joinAddrs(to)))
		},
	}
	cmd.Flags().StringArrayVar(&to, "to", nil, "recipient address (repeatable)")
	cmd.Flags().StringArrayVar(&cc, "cc", nil, "cc address (repeatable)")
	cmd.Flags().StringArrayVar(&bcc, "bcc", nil, "bcc address, hidden from every other recipient (repeatable)")
	cmd.Flags().StringVar(&subject, "subject", "", "message subject")
	cmd.Flags().StringVar(&body, "body", "", "message body (default: read from stdin)")
	cmd.Flags().StringArrayVar(&attach, "attach", nil, "path to a file to attach (repeatable)")
	df.register(cmd)
	return cmd
}

func newComposeReplyCmd() *cobra.Command {
	var folderPath, subject, body string
	var replyAll bool
	var attach, bcc []string
	var df draftFlags
	cmd := &cobra.Command{
		Use:   "reply <slug> <uid>",
		Short: "Reply to a cached message",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			uid64, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid uid %q: %w", args[1], err)
			}
			uid := uint32(uid64)
			attachments, err := loadAttachments(attach)
			if err != nil {
				return err
			}

			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			cfg, err := acctSvc.Get(cmd.Context(), slug)
			if err != nil {
				return err
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			orig, err := findMessage(cmd, st, slug, folderPath, uid)
			if err != nil {
				return err
			}

			origBody, err := st.Message.Body(cmd.Context(), cfg, folderPath, uid)
			if err != nil {
				return err
			}

			bodyText := body
			if bodyText == "" {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read body from stdin: %w", err)
				}
				bodyText = string(b)
			}

			composeSvc := compose.NewService(st.Folder, st.Signature)
			draft := composeSvc.NewReply(cmd.Context(), cfg, orig, origBody, replyAll)
			if subject != "" {
				draft.Subject = subject
			}
			draft.Body = joinBody(bodyText, draft.Body)
			draft.Attachments = attachments
			draft.Bcc = bcc

			return df.finish(cmd, composeSvc, cfg, draft, fmt.Sprintf("Reply sent to %s.", joinAddrs(draft.To)))
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the message lives in")
	cmd.Flags().BoolVar(&replyAll, "all", false, "reply to all original recipients, not just the sender")
	cmd.Flags().StringVar(&subject, "subject", "", "override the default \"Re: ...\" subject")
	cmd.Flags().StringVar(&body, "body", "", "text to add above the quoted original (default: read from stdin)")
	cmd.Flags().StringArrayVar(&attach, "attach", nil, "path to a file to attach (repeatable)")
	cmd.Flags().StringArrayVar(&bcc, "bcc", nil, "bcc address, hidden from every other recipient (repeatable)")
	df.register(cmd)
	return cmd
}

func newComposeForwardCmd() *cobra.Command {
	var folderPath, subject, body string
	var to, cc, bcc, attach []string
	var df draftFlags
	cmd := &cobra.Command{
		Use:   "forward <slug> <uid>",
		Short: "Forward a cached message",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			uid64, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid uid %q: %w", args[1], err)
			}
			uid := uint32(uid64)
			if len(to) == 0 && !df.asDraft {
				return fmt.Errorf("at least one --to recipient is required (or pass --draft to save one without recipients yet)")
			}
			attachments, err := loadAttachments(attach)
			if err != nil {
				return err
			}

			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			cfg, err := acctSvc.Get(cmd.Context(), slug)
			if err != nil {
				return err
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			orig, err := findMessage(cmd, st, slug, folderPath, uid)
			if err != nil {
				return err
			}

			origBody, err := st.Message.Body(cmd.Context(), cfg, folderPath, uid)
			if err != nil {
				return err
			}

			bodyText := body
			if bodyText == "" {
				b, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("read body from stdin: %w", err)
				}
				bodyText = string(b)
			}

			composeSvc := compose.NewService(st.Folder, st.Signature)
			draft := composeSvc.NewForward(cmd.Context(), cfg, orig, origBody)
			draft.To = to
			draft.Cc = cc
			draft.Bcc = bcc
			if subject != "" {
				draft.Subject = subject
			}
			draft.Body = joinBody(bodyText, draft.Body)
			// The forwarded attachments NewForward already carried over come
			// first; --attach adds more on top rather than replacing them.
			draft.Attachments = append(draft.Attachments, attachments...)

			return df.finish(cmd, composeSvc, cfg, draft, fmt.Sprintf("Forwarded to %s.", joinAddrs(to)))
		},
	}
	cmd.Flags().StringVar(&folderPath, "folder", defaultListFolder, "folder the message lives in")
	cmd.Flags().StringArrayVar(&to, "to", nil, "recipient address (repeatable)")
	cmd.Flags().StringArrayVar(&cc, "cc", nil, "cc address (repeatable)")
	cmd.Flags().StringArrayVar(&bcc, "bcc", nil, "bcc address, hidden from every other recipient (repeatable)")
	cmd.Flags().StringVar(&subject, "subject", "", "override the default \"Fwd: ...\" subject")
	cmd.Flags().StringVar(&body, "body", "", "text to add above the forwarded message (default: read from stdin)")
	cmd.Flags().StringArrayVar(&attach, "attach", nil, "path to an extra file to attach, in addition to the original's own (repeatable)")
	df.register(cmd)
	return cmd
}

func newComposeDraftsCmd() *cobra.Command {
	var draftFolder string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "drafts <slug>",
		Short: "List saved drafts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			if _, err := acctSvc.Get(cmd.Context(), slug); err != nil {
				return err
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			composeSvc := compose.NewService(st.Folder, st.Signature)
			path := draftFolder
			if path == "" {
				path, err = composeSvc.DraftsFolder(cmd.Context(), slug)
				if err != nil {
					return err
				}
			}

			msgs, err := st.Message.List(cmd.Context(), slug, path)
			if err != nil {
				return err
			}
			return renderMessages(cmd, msgs, jsonOut, fmt.Sprintf("No drafts saved in %q.", path))
		},
	}
	cmd.Flags().StringVar(&draftFolder, "folder", "", "the Drafts folder to list (default: auto-detect)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON instead of a table")
	return cmd
}

func newComposeSendDraftCmd() *cobra.Command {
	var draftFolder, subject, body string
	var to, cc, bcc, attach []string
	cmd := &cobra.Command{
		Use:   "send-draft <slug> <uid>",
		Short: "Send a previously saved draft",
		Long: "Send a previously saved draft, then remove it from the Drafts folder.\n\n" +
			"Any of --to/--cc/--bcc/--subject/--body given here replaces the draft's own; " +
			"--attach adds to the draft's own attachments rather than replacing them.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			uid64, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid uid %q: %w", args[1], err)
			}
			uid := uint32(uid64)
			extra, err := loadAttachments(attach)
			if err != nil {
				return err
			}

			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			cfg, err := acctSvc.Get(cmd.Context(), slug)
			if err != nil {
				return err
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			composeSvc := compose.NewService(st.Folder, st.Signature)
			path := draftFolder
			if path == "" {
				path, err = composeSvc.DraftsFolder(cmd.Context(), slug)
				if err != nil {
					return err
				}
			}

			orig, err := findMessage(cmd, st, slug, path, uid)
			if err != nil {
				return err
			}
			origBody, err := st.Message.Body(cmd.Context(), cfg, path, uid)
			if err != nil {
				return err
			}

			draft := composeSvc.LoadDraft(orig, origBody)
			if len(to) > 0 {
				draft.To = to
			}
			if len(cc) > 0 {
				draft.Cc = cc
			}
			if len(bcc) > 0 {
				draft.Bcc = bcc
			}
			if subject != "" {
				draft.Subject = subject
			}
			if body != "" {
				draft.Body = body
			}
			draft.Attachments = append(draft.Attachments, extra...)
			if len(draft.To) == 0 {
				return fmt.Errorf("draft %d has no recipients; pass --to", uid)
			}

			if err := composeSvc.Send(cmd.Context(), cfg, draft); err != nil {
				return err
			}
			if err := composeSvc.DiscardDraft(cmd.Context(), cfg, compose.DraftRef{FolderPath: path, UID: uid}); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: sent, but couldn't remove the draft copy: %v\n", err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Draft sent to %s.\n", joinAddrs(draft.To))
			return nil
		},
	}
	cmd.Flags().StringVar(&draftFolder, "folder", "", "the draft's folder (default: auto-detect the Drafts folder)")
	cmd.Flags().StringArrayVar(&to, "to", nil, "override recipient address (repeatable)")
	cmd.Flags().StringArrayVar(&cc, "cc", nil, "override cc address (repeatable)")
	cmd.Flags().StringArrayVar(&bcc, "bcc", nil, "override bcc address (repeatable)")
	cmd.Flags().StringVar(&subject, "subject", "", "override the draft's subject")
	cmd.Flags().StringVar(&body, "body", "", "override the draft's body")
	cmd.Flags().StringArrayVar(&attach, "attach", nil, "path to an extra file to attach, in addition to the draft's own (repeatable)")
	return cmd
}

func newComposeDiscardDraftCmd() *cobra.Command {
	var draftFolder string
	cmd := &cobra.Command{
		Use:   "discard-draft <slug> <uid>",
		Short: "Permanently delete a saved draft",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			uid64, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid uid %q: %w", args[1], err)
			}
			uid := uint32(uid64)

			acctSvc, err := newAccountService()
			if err != nil {
				return err
			}
			cfg, err := acctSvc.Get(cmd.Context(), slug)
			if err != nil {
				return err
			}

			st, err := openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()

			composeSvc := compose.NewService(st.Folder, st.Signature)
			path := draftFolder
			if path == "" {
				path, err = composeSvc.DraftsFolder(cmd.Context(), slug)
				if err != nil {
					return err
				}
			}

			if err := composeSvc.DiscardDraft(cmd.Context(), cfg, compose.DraftRef{FolderPath: path, UID: uid}); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Draft %d discarded.\n", uid)
			return nil
		},
	}
	cmd.Flags().StringVar(&draftFolder, "folder", "", "the draft's folder (default: auto-detect the Drafts folder)")
	return cmd
}

// findMessage looks up uid in slug/folderPath among the folder's cached
// headers — the shared lookup reply, forward, and send-draft each do
// before building their draft.
func findMessage(cmd *cobra.Command, st *core.Store, slug, folderPath string, uid uint32) (message.Message, error) {
	msgs, err := st.Message.List(cmd.Context(), slug, folderPath)
	if err != nil {
		return message.Message{}, err
	}
	for _, m := range msgs {
		if m.UID == uid {
			return m, nil
		}
	}
	return message.Message{}, fmt.Errorf("message %d not found in %q", uid, folderPath)
}

// loadAttachments reads each path in paths into a compose.Attachment.
func loadAttachments(paths []string) ([]compose.Attachment, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]compose.Attachment, len(paths))
	for i, p := range paths {
		att, err := compose.NewAttachmentFromFile(p)
		if err != nil {
			return nil, fmt.Errorf("attach %s: %w", p, err)
		}
		out[i] = att
	}
	return out, nil
}

// joinBody combines a user-supplied body with the account's default
// signature (already formatted as "-- \n<sig>\n" by core/compose, or empty
// if none is configured).
func joinBody(text, signature string) string {
	if signature == "" {
		return text
	}
	if text == "" {
		return signature
	}
	return text + "\n" + signature
}
