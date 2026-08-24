// Package smtp is a thin wrapper over emersion/go-smtp's client package:
// dialing, STARTTLS, auth via internal/auth.Provider, and sending built
// MIME messages. Consumed only by internal/core/compose.
//
// Implemented starting in Phase 1 (dial/auth for `account test`) and
// extended in Phase 4 (send).
package smtp
