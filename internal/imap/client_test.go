package imap

import (
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func uidSet(uids ...imap.UID) imap.UIDSet { return imap.UIDSetNum(uids...) }

func TestMoveResultFromData(t *testing.T) {
	t.Run("pairs source and destination UIDs positionally", func(t *testing.T) {
		req := uidSet(1, 4)
		res := moveResultFromData(req, &imapclient.MoveData{
			UIDValidity: 7, SourceUIDs: uidSet(1, 4), DestUIDs: uidSet(2, 3),
		})
		if res.UIDValidity != 7 || len(res.Dest) != 2 || res.Dest[1] != 2 || res.Dest[4] != 3 {
			t.Fatalf("got %+v, want 1->2 and 4->3 with validity 7", res)
		}
	})

	untrusted := map[string]struct {
		req  imap.UIDSet
		data *imapclient.MoveData
	}{
		"no COPYUID data": {uidSet(1), nil},
		"empty response":  {uidSet(1), &imapclient.MoveData{}},
		"count mismatch":  {uidSet(1, 2), &imapclient.MoveData{UIDValidity: 1, SourceUIDs: uidSet(1, 2), DestUIDs: uidSet(9)}},
		"source set differs from what was requested": {
			uidSet(1, 2), &imapclient.MoveData{UIDValidity: 1, SourceUIDs: uidSet(1, 3), DestUIDs: uidSet(8, 9)},
		},
		"fewer sources than requested": {
			uidSet(1, 2), &imapclient.MoveData{UIDValidity: 1, SourceUIDs: uidSet(1), DestUIDs: uidSet(9)},
		},
	}
	for name, c := range untrusted {
		t.Run(name, func(t *testing.T) {
			if res := moveResultFromData(c.req, c.data); len(res.Dest) != 0 {
				t.Fatalf("Dest = %v, want empty (mapping can't be trusted)", res.Dest)
			}
		})
	}
}
