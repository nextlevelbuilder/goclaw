package protocol

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// LoginWithCredentials performs cookie-based re-login using saved credentials.
// Concurrent: fetches login info (encrypted) + server info in parallel.
func LoginWithCredentials(ctx context.Context, sess *Session, cred Credentials) error {
	if !cred.IsValid() {
		return fmt.Errorf("zalo_personal: invalid credentials")
	}

	lang := DefaultLanguage
	if cred.Language != nil && *cred.Language != "" {
		lang = *cred.Language
	}
	sess.IMEI = cred.IMEI
	sess.UserAgent = cred.UserAgent
	sess.Language = lang

	if cred.Cookie != nil && cred.Cookie.IsValid() {
		cred.Cookie.BuildCookieJar(&DefaultBaseURL, sess.CookieJar)
	}

	var (
		loginInfo  *LoginInfo
		serverInfo *ServerInfo
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		li, err := fetchLoginInfo(gctx, sess)
		if err != nil {
			return fmt.Errorf("zalo_personal: login: %w", err)
		}
		loginInfo = li
		return nil
	})
	g.Go(func() error {
		si, err := fetchServerInfo(gctx, sess)
		if err != nil {
			return fmt.Errorf("zalo_personal: server info: %w", err)
		}
		serverInfo = si
		return nil
	})

	if err := g.Wait(); err != nil {
		return err
	}
	if loginInfo == nil || serverInfo == nil {
		return fmt.Errorf("zalo_personal: login failed (empty response)")
	}

	sess.UID = loginInfo.UID
	sess.SecretKey = loginInfo.ZPWEnk
	sess.LoginInfo = loginInfo
	sess.Settings = serverInfo.Settings

	// Seed cookies for every service-map host so that API calls to
	// subdomains like tt-group-poll-wpa.chat.zalo.me carry zpw_sek.
	// Go's net/http/cookiejar does not propagate cookies across subdomains.
	if cred.Cookie != nil && cred.Cookie.IsValid() {
		seedServiceMapCookies(sess, cred)
	}

	return nil
}

// seedServiceMapCookies seeds the session cookie jar for every distinct host
// found in the service map. This is necessary because Go's net/http/cookiejar
// does not propagate cookies across subdomains (e.g. chat.zalo.me cookies are
// not sent to tt-group-poll-wpa.chat.zalo.me).
func seedServiceMapCookies(sess *Session, cred Credentials) {
	if sess.LoginInfo == nil || cred.Cookie == nil {
		return
	}
	sm := sess.LoginInfo.ZpwServiceMapV3
	allURLs := make([]string, 0, 16)
	allURLs = append(allURLs, sm.Chat...)
	allURLs = append(allURLs, sm.Group...)
	allURLs = append(allURLs, sm.File...)
	allURLs = append(allURLs, sm.Profile...)
	allURLs = append(allURLs, sm.GroupPoll...)

	seen := make(map[string]struct{}, len(allURLs))
	for _, raw := range allURLs {
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			continue
		}
		host := parsed.Scheme + "://" + parsed.Host
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		u := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}
		cred.Cookie.BuildCookieJar(u, sess.CookieJar)
	}
}

// LoginQR performs interactive QR code login.
// qrCallback is called with QR image bytes (PNG) for display.
// Returns credentials for persistence on success.
func LoginQR(ctx context.Context, sess *Session, qrCallback func(qrPNG []byte)) (*Credentials, error) {
	ver, err := loadLoginPage(ctx, sess)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: load login page: %w", err)
	}

	qrGetLoginInfo(ctx, sess, ver)
	qrVerifyClient(ctx, sess, ver)

	qrData, imgBytes, err := qrGenerateCode(ctx, sess, ver)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: generate qr: %w", err)
	}

	if qrCallback != nil {
		qrCallback(imgBytes)
	}

	// Wait for scan (long-poll, retry on error_code=8)
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()

	if err := qrWaitingScan(timeoutCtx, sess, ver, qrData.Code); err != nil {
		return nil, fmt.Errorf("zalo_personal: waiting scan: %w", err)
	}

	// Wait for confirm (long-poll)
	if err := qrWaitingConfirm(timeoutCtx, sess, ver, qrData.Code); err != nil {
		return nil, fmt.Errorf("zalo_personal: waiting confirm: %w", err)
	}

	// Validate session
	if err := qrCheckSession(ctx, sess); err != nil {
		return nil, fmt.Errorf("zalo_personal: check session: %w", err)
	}

	userInfo, err := qrGetUserInfo(ctx, sess)
	if err != nil || !userInfo.Logged {
		return nil, fmt.Errorf("zalo_personal: get user info failed or not logged in")
	}

	// Build credentials from session cookies for persistence
	imei := GenerateIMEI(sess.UserAgent)
	lang := sess.Language
	cookies := NewHTTPCookies(sess.CookieJar.Cookies(&DefaultBaseURL))
	cred := &Credentials{
		IMEI:      imei,
		UserAgent: sess.UserAgent,
		Cookie:    &cookies,
		Language:  &lang,
	}

	// Credentials are validated when the channel starts via LoginWithCredentials.
	// Calling it here would conflict with the active QR session state.
	return cred, nil
}

// --- Cookie login helpers ---

func fetchLoginInfo(ctx context.Context, sess *Session) (*LoginInfo, error) {
	params, enk, err := getEncryptParam(sess, "getlogininfo")
	if err != nil {
		return nil, err
	}

	params["nretry"] = 0
	u := makeURL(sess, "https://wpa.chat.zalo.me/api/login/getLoginInfo", params, true)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	setDefaultHeaders(req, sess)

	resp, err := sess.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var base Response[*string]
	if err := readJSON(resp, &base); err != nil {
		return nil, fmt.Errorf("parse login response: %w", err)
	}

	if enk == nil || base.Data == nil {
		return nil, fmt.Errorf("no encrypted data in response")
	}

	// Decrypt response
	unescaped, err := url.PathUnescape(*base.Data)
	if err != nil {
		return nil, err
	}
	plain, err := DecodeAESCBC([]byte(*enk), unescaped)
	if err != nil {
		return nil, fmt.Errorf("decrypt login data: %w", err)
	}

	var obj Response[*LoginInfo]
	if err := json.Unmarshal(plain, &obj); err != nil {
		return nil, err
	}
	return obj.Data, nil
}

func fetchServerInfo(ctx context.Context, sess *Session) (*ServerInfo, error) {
	params, _, err := getEncryptParam(sess, "getserverinfo")
	if err != nil {
		return nil, err
	}

	// For getserverinfo, only pass signkey + basic params (no encrypted body)
	signkey, _ := params["signkey"].(string)
	siParams := map[string]any{
		"signkey":        signkey,
		"imei":           sess.IMEI,
		"type":           DefaultAPIType,
		"client_version": DefaultAPIVersion,
		"computer_name":  DefaultComputerName,
	}

	u := makeURL(sess, "https://wpa.chat.zalo.me/api/login/getServerInfo", siParams, false)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	setDefaultHeaders(req, sess)

	resp, err := sess.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// ServerInfo uses a different response shape
	var result struct {
		Data *ServerInfo `json:"data"`
	}
	if err := readJSON(resp, &result); err != nil {
		return nil, fmt.Errorf("parse server info: %w", err)
	}
	return result.Data, nil
}

// --- QR login helpers ---

func loadLoginPage(ctx context.Context, sess *Session) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://id.zalo.me/account?continue=https%3A%2F%2Fchat.zalo.me%2F", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", sess.UserAgent)

	resp, err := sess.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`https:\/\/stc-zlogin\.zdn\.vn\/main-([\d.]+)\.js`)
	match := re.FindSubmatch(body)
	if len(match) < 2 {
		return "", fmt.Errorf("zalo_personal: version not found in login page HTML")
	}
	return string(match[1]), nil
}

var qrHeaders = http.Header{
	"Accept":          {"*/*"},
	"Content-Type":    {"application/x-www-form-urlencoded"},
	"Sec-Fetch-Dest":  {"empty"},
	"Sec-Fetch-Mode":  {"cors"},
	"Sec-Fetch-Site":  {"same-origin"},
	"Referer":         {"https://id.zalo.me/account?continue=https%3A%2F%2Fzalo.me%2Fpc"},
	"Referrer-Policy": {"strict-origin-when-cross-origin"},
}

func qrPost(ctx context.Context, sess *Session, endpoint string, formData map[string]string) (*http.Response, error) {
	body := buildFormBody(formData)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", sess.UserAgent)
	maps.Copy(req.Header, qrHeaders)
	return sess.Client.Do(req)
}

func qrGetLoginInfo(ctx context.Context, sess *Session, ver string) {
	resp, err := qrPost(ctx, sess, "https://id.zalo.me/account/logininfo", map[string]string{
		"v": ver, "continue": "https://zalo.me/pc",
	})
	if err == nil {
		resp.Body.Close()
	}
}

func qrVerifyClient(ctx context.Context, sess *Session, ver string) {
	resp, err := qrPost(ctx, sess, "https://id.zalo.me/account/verify-client", map[string]string{
		"v": ver, "type": "device", "continue": "https://zalo.me/pc",
	})
	if err == nil {
		resp.Body.Close()
	}
}

func qrGenerateCode(ctx context.Context, sess *Session, ver string) (*QRGeneratedData, []byte, error) {
	resp, err := qrPost(ctx, sess, "https://id.zalo.me/account/authen/qr/generate", map[string]string{
		"v": ver, "continue": "https://zalo.me/pc",
	})
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	var body Response[QRGeneratedData]
	if err := readJSON(resp, &body); err != nil {
		return nil, nil, err
	}

	b64 := strings.TrimPrefix(body.Data.Image, "data:image/png;base64,")
	imgBytes, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil, err
	}
	return &body.Data, imgBytes, nil
}

func qrWaitingScan(ctx context.Context, sess *Session, ver, code string) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		resp, err := qrPost(ctx, sess, "https://id.zalo.me/account/authen/qr/waiting-scan", map[string]string{
			"v": ver, "code": code, "continue": "https://zalo.me/pc",
		})
		if err != nil {
			return err
		}
		var body Response[QRScannedData]
		if err := readJSON(resp, &body); err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		if body.ErrorCode == 8 {
			continue // not ready yet, retry
		}
		if body.ErrorCode != 0 {
			return fmt.Errorf("zalo_personal: scan error code %d: %s", body.ErrorCode, body.ErrorMessage)
		}
		return nil
	}
}

func qrWaitingConfirm(ctx context.Context, sess *Session, ver, code string) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		resp, err := qrPost(ctx, sess, "https://id.zalo.me/account/authen/qr/waiting-confirm", map[string]string{
			"v": ver, "code": code, "gToken": "", "gAction": "CONFIRM_QR", "continue": "https://zalo.me/pc",
		})
		if err != nil {
			return err
		}
		var body Response[struct{}]
		if err := readJSON(resp, &body); err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		if body.ErrorCode == 8 {
			continue // not ready yet, retry
		}
		if body.ErrorCode == -13 {
			return fmt.Errorf("zalo_personal: QR login declined by user")
		}
		if body.ErrorCode != 0 {
			return fmt.Errorf("zalo_personal: confirm error code %d: %s", body.ErrorCode, body.ErrorMessage)
		}
		return nil
	}
}

func qrCheckSession(ctx context.Context, sess *Session) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://id.zalo.me/account/checksession?continue=https%3A%2F%2Fchat.zalo.me%2Findex.html", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", sess.UserAgent)

	resp, err := sess.Client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func qrGetUserInfo(ctx context.Context, sess *Session) (*QRUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://jr.chat.zalo.me/jr/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", sess.UserAgent)
	req.Header.Set("Referer", DefaultBaseURL.String()+"/")

	resp, err := sess.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body Response[QRUserInfo]
	if err := readJSON(resp, &body); err != nil {
		return nil, err
	}
	return &body.Data, nil
}

// --- Shared helpers ---

func setDefaultHeaders(req *http.Request, sess *Session) {
	h := defaultHeaders(sess)
	maps.Copy(req.Header, h)
}

// readJSON decodes a potentially gzip/deflate-encoded JSON response.
func readJSON(resp *http.Response, target any) error {
	var reader io.ReadCloser
	var err error

	switch strings.ToLower(resp.Header.Get("Content-Encoding")) {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		defer reader.Close()
	default:
		reader = resp.Body
	}

	return json.NewDecoder(reader).Decode(target)
}
