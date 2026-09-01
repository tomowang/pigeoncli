// Package settings is a core domain/service layer package. It exposes
// Service, the user-preferences API (background sync interval, initial
// sync window, theme) used by internal/cli and internal/tui, so they read
// preferences from this package instead of importing internal/config
// directly.
package settings
