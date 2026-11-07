package browser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Sources capture_secret reads from.
const (
	captureLocalStorage   = "localStorage"
	captureSessionStorage = "sessionStorage"
	captureCookie         = "cookie"
)

const maxCaptureKeyLen = 256

// CaptureSecretOpts describes one capture_secret act.
type CaptureSecretOpts struct {
	Source     string // localStorage | sessionStorage | cookie
	Key        string // storage key or cookie name
	SecretName string // name of the new vault entry
	SecretKind string // only api_key; "" means api_key
}

var (
	// ErrCaptureUnscoped refuses a capture outside a browser context isolated
	// to this run's agent (for example the shared main Chrome).
	ErrCaptureUnscoped = errors.New("capture_secret needs a browser context isolated to this agent")
	errCaptureTooLarge = errors.New("value too large")
)

// CaptureSecret reads a value from the page's storage or cookies and writes it
// straight into the vault as a pending entry of this run's agent. The value is
// read here, never returned, logged, put in an error or passed through
// evaluate. The allowed domain is the page host, taken from the page itself.
// It returns the write status (secretvault.WriteActive or WritePending).
func (m *Manager) CaptureSecret(ctx context.Context, targetID string, opts CaptureSecretOpts) (string, error) {
	rt := secretvault.RuntimeFrom(ctx)
	if rt == nil {
		return "", errors.New("secret vault is not available in this run")
	}
	if err := rt.CheckWrite(ctx, opts.SecretName); err != nil {
		return "", err
	}
	if err := validateCaptureOpts(opts); err != nil {
		rt.AuditWriteRefusal(ctx, opts.SecretName, "agent_capture_invalid")
		return "", err
	}
	scope := scopeFromCtx(ctx)
	if scope.usesMainBrowser() || scope.AgentID != rt.AgentID.String() {
		rt.AuditWriteRefusal(ctx, opts.SecretName, "agent_capture_unscoped_browser")
		return "", ErrCaptureUnscoped
	}

	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, scope.Key())
	m.mu.Unlock()
	if err != nil {
		return "", err
	}
	info, err := page.Info()
	if err != nil || info == nil {
		return "", errors.New("cannot read the page address")
	}
	page = page.Context(ctx)
	return captureSecret(ctx, rt, info.URL, opts, func() (pageRead, error) {
		return readPageSecret(page, info.URL, opts)
	})
}

// pageRead is what one read of the page returned. origin is the origin of the
// document the value actually came from ("scheme://host[:port]").
type pageRead struct {
	value  string
	found  bool
	origin string
}

var errCaptureOriginChanged = errors.New("the page changed while its storage was read; open the page again and retry")

// captureSecret is the page-independent part of CaptureSecret: origin and
// name checks before reading, then the vault write. The tab can navigate
// between the address check and the read, so the origin the read reports is
// checked again and must equal the one the allowed domain was taken from;
// otherwise the value is dropped.
func captureSecret(ctx context.Context, rt *secretvault.Runtime, pageURL string, opts CaptureSecretOpts,
	read func() (pageRead, error)) (string, error) {
	host, err := checkFillOrigin(pageURL)
	if err != nil {
		rt.AuditWriteRefusal(ctx, opts.SecretName, "agent_capture_bad_origin")
		return "", errors.New("secrets can only be captured from https pages (or http on localhost)")
	}
	checked, _, _ := topOrigin(pageURL) // cannot fail: checkFillOrigin parsed it
	ns := secretvault.NewSecret{Name: opts.SecretName, Kind: store.SecretKindAPIKey, Domains: []string{host}}
	if _, err := secretvault.ValidateNewSecret(ns); err != nil {
		rt.AuditWriteRefusal(ctx, opts.SecretName, "agent_invalid")
		return "", err
	}

	got, err := read()
	switch {
	case errors.Is(err, errCaptureTooLarge):
		rt.AuditWriteRefusal(ctx, opts.SecretName, "agent_value_too_large")
		return "", secretvault.ErrValueTooLarge
	case err != nil:
		// Never echo the driver error: it could carry page data.
		rt.AuditWriteRefusal(ctx, opts.SecretName, "agent_capture_read_failed")
		return "", errors.New("could not read the page storage")
	case !sameCaptureOrigin(checked, got.origin):
		rt.AuditWriteRefusal(ctx, opts.SecretName, "agent_capture_origin_changed")
		return "", errCaptureOriginChanged
	case !got.found || got.value == "":
		rt.AuditWriteRefusal(ctx, opts.SecretName, "agent_capture_not_found")
		return "", fmt.Errorf("no %s value named %q on this page", opts.Source, opts.Key)
	}
	return rt.CreatePending(ctx, ns, store.SecretVaultPayload{Secret: got.value})
}

// sameCaptureOrigin reports whether the origin a read reported passes the
// capture origin rule and equals the origin that was checked before reading.
func sameCaptureOrigin(checked, readOrigin string) bool {
	if _, err := checkFillOrigin(readOrigin); err != nil {
		return false
	}
	o, _, err := topOrigin(readOrigin)
	return err == nil && o == checked
}

func validateCaptureOpts(opts CaptureSecretOpts) error {
	switch opts.Source {
	case captureLocalStorage, captureSessionStorage, captureCookie:
	default:
		return errors.New(`source must be "localStorage", "sessionStorage" or "cookie"`)
	}
	if opts.SecretKind != "" && opts.SecretKind != store.SecretKindAPIKey {
		return errors.New(`capture_secret stores single values only: secret_kind must be "api_key"`)
	}
	if opts.Key == "" || len(opts.Key) > maxCaptureKeyLen || !utf8.ValidString(opts.Key) ||
		strings.IndexFunc(opts.Key, unicode.IsControl) >= 0 {
		return fmt.Errorf("key must be 1-%d printable characters", maxCaptureKeyLen)
	}
	if opts.SecretName == "" {
		return errors.New("secret_name is required")
	}
	return nil
}

// readStorageJS reads one storage item in the top frame, together with the
// protocol and host of the document it read from. It returns the value only
// when it fits the size cap, so an oversized value never leaves the page.
const readStorageJS = `(src, key, max) => {
	const origin = location.protocol + '//' + location.host;
	const s = src === 'sessionStorage' ? window.sessionStorage : window.localStorage;
	const v = s.getItem(key);
	if (v === null) return { origin, found: false };
	if (v.length > max) return { origin, found: true, tooLarge: true };
	return { origin, found: true, value: v };
}`

// readPageSecret reads the value in the browser: storage through a fixed
// function in the page's top frame, cookies (including HttpOnly) through CDP
// for the page URL. Cookies are requested for pageURL explicitly, so their
// origin is that URL's whatever the tab shows by then.
func readPageSecret(page *rod.Page, pageURL string, opts CaptureSecretOpts) (pageRead, error) {
	if opts.Source == captureCookie {
		res, err := proto.NetworkGetCookies{Urls: []string{pageURL}}.Call(page)
		if err != nil {
			return pageRead{}, err
		}
		for _, c := range res.Cookies {
			if c.Name == opts.Key {
				if len(c.Value) > secretvault.MaxWriteValueBytes {
					return pageRead{}, errCaptureTooLarge
				}
				return pageRead{value: c.Value, found: true, origin: pageURL}, nil
			}
		}
		return pageRead{origin: pageURL}, nil
	}
	res, err := page.Eval(readStorageJS, opts.Source, opts.Key, secretvault.MaxWriteValueBytes)
	if err != nil {
		return pageRead{}, err
	}
	obj := res.Value
	if obj.Get("tooLarge").Bool() {
		return pageRead{}, errCaptureTooLarge
	}
	return pageRead{value: obj.Get("value").Str(), found: obj.Get("found").Bool(), origin: obj.Get("origin").Str()}, nil
}
