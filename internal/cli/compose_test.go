package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/tomowang/pigeoncli/internal/config"
	"github.com/tomowang/pigeoncli/internal/core/compose"
)

// newTestCmd returns a bare *cobra.Command with a real, non-nil context —
// cmd.Context() is only ever nil like this when a Command is constructed
// directly rather than run through Execute()/ExecuteContext(), which never
// happens outside a test.
func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return cmd
}

func TestDraftFlagsReplaceRequiresDraft(t *testing.T) {
	df := draftFlags{replaceUID: 5}
	cmd := newTestCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := df.finish(cmd, compose.NewService(nil, nil), config.Account{}, compose.Draft{}, "sent")
	if err == nil || !strings.Contains(err.Error(), "--replace requires --draft") {
		t.Fatalf("finish() = %v, want a --replace-requires---draft error", err)
	}
}

func TestDraftFlagsWithoutDraftCallsSend(t *testing.T) {
	// Send dials a real IMAP/SMTP server, which this test doesn't stand up,
	// so it's expected to fail — but it must be Send that fails, not the
	// --replace validation (asDraft is false, replaceUID is zero).
	df := draftFlags{}
	cmd := newTestCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := df.finish(cmd, compose.NewService(nil, nil), config.Account{}, compose.Draft{}, "sent")
	if err == nil {
		t.Fatal("expected Send to fail without a real account/server")
	}
	if strings.Contains(err.Error(), "--replace") {
		t.Fatalf("got the --replace validation error instead of a Send failure: %v", err)
	}
}
