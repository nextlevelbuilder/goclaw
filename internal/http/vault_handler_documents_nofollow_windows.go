//go:build windows

package http

// Windows has no O_NOFOLLOW concept and follows reparse points transparently;
// the Lstat short-circuit in writeDocumentContent is the only protection
// available there. Desktop edition is single-user with no untrusted tenants,
// so this degradation is acceptable.
const oNoFollow = 0

func isSymlinkLoopErr(err error) bool { return false }
