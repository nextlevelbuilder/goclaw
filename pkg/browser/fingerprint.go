package browser

import (
	"fmt"
	"math/rand"
	"strings"
)

// Fingerprint represents a consistent browser fingerprint profile.
// All fields are coordinated to avoid detection (e.g. UA matches platform/timezone).
type Fingerprint struct {
	UserAgent           string   `json:"userAgent"`
	Platform            string   `json:"platform"`
	Vendor              string   `json:"vendor"`
	Languages           []string `json:"languages"`
	ScreenWidth         int      `json:"screenWidth"`
	ScreenHeight        int      `json:"screenHeight"`
	ColorDepth          int      `json:"colorDepth"`
	Timezone            string   `json:"timezone"`
	HardwareConcurrency int      `json:"hardwareConcurrency"`
	DeviceMemory        int      `json:"deviceMemory"`
	MaxTouchPoints      int      `json:"maxTouchPoints"`
	WebGLVendor         string   `json:"webglVendor"`
	WebGLRenderer       string   `json:"webglRenderer"`
}

// fingerprintProfile is a pre-built consistent profile.
type fingerprintProfile struct {
	UA       string
	Platform string
	Timezone string
	Lang     []string
	Screen   [2]int    // width, height
	WebGL    [2]string // vendor, renderer
}

// Pre-built fingerprint profiles — all fields are consistent within each profile.
var fingerprintProfiles = []fingerprintProfile{
	{
		UA:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
		Platform: "Win32", Timezone: "America/New_York", Lang: []string{"en-US", "en"},
		Screen: [2]int{1920, 1080}, WebGL: [2]string{"Google Inc. (NVIDIA)", "ANGLE (NVIDIA, NVIDIA GeForce GTX 1080 Direct3D11 vs_5_0 ps_5_0)"},
	},
	{
		UA:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
		Platform: "Win32", Timezone: "America/Los_Angeles", Lang: []string{"en-US", "en"},
		Screen: [2]int{2560, 1440}, WebGL: [2]string{"Google Inc. (AMD)", "ANGLE (AMD, AMD Radeon RX 580 Direct3D11 vs_5_0 ps_5_0)"},
	},
	{
		UA:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
		Platform: "MacIntel", Timezone: "America/Chicago", Lang: []string{"en-US", "en"},
		Screen: [2]int{1440, 900}, WebGL: [2]string{"Apple", "Apple GPU"},
	},
	{
		UA:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
		Platform: "MacIntel", Timezone: "Europe/London", Lang: []string{"en-GB", "en"},
		Screen: [2]int{1680, 1050}, WebGL: [2]string{"Apple", "Apple M1"},
	},
	{
		UA:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
		Platform: "Win32", Timezone: "Asia/Ho_Chi_Minh", Lang: []string{"vi-VN", "vi", "en"},
		Screen: [2]int{1920, 1080}, WebGL: [2]string{"Google Inc. (NVIDIA)", "ANGLE (NVIDIA, NVIDIA GeForce GTX 1060 Direct3D11 vs_5_0 ps_5_0)"},
	},
	{
		UA:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
		Platform: "Win32", Timezone: "Asia/Tokyo", Lang: []string{"ja", "en"},
		Screen: [2]int{1920, 1080}, WebGL: [2]string{"Google Inc. (NVIDIA)", "ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Direct3D11 vs_5_0 ps_5_0)"},
	},
	{
		UA:       "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
		Platform: "Linux x86_64", Timezone: "Europe/Berlin", Lang: []string{"de-DE", "de", "en"},
		Screen: [2]int{1920, 1080}, WebGL: [2]string{"Google Inc. (Intel)", "ANGLE (Intel, Mesa Intel(R) UHD Graphics 630)"},
	},
}

// GenerateFingerprint creates a consistent fingerprint.
// If geoHint is provided, it prefers profiles matching that timezone region.
func GenerateFingerprint(geoHint string) *Fingerprint {
	// Try to find a matching profile by geo hint
	var candidates []fingerprintProfile
	if geoHint != "" {
		for _, p := range fingerprintProfiles {
			if matchGeo(p.Timezone, geoHint) {
				candidates = append(candidates, p)
			}
		}
	}
	if len(candidates) == 0 {
		candidates = fingerprintProfiles
	}

	p := candidates[rand.Intn(len(candidates))]

	cores := []int{4, 8, 12, 16}
	memory := []int{4, 8, 16}

	return &Fingerprint{
		UserAgent:           p.UA,
		Platform:            p.Platform,
		Vendor:              "Google Inc.",
		Languages:           p.Lang,
		ScreenWidth:         p.Screen[0],
		ScreenHeight:        p.Screen[1],
		ColorDepth:          24,
		Timezone:            p.Timezone,
		HardwareConcurrency: cores[rand.Intn(len(cores))],
		DeviceMemory:        memory[rand.Intn(len(memory))],
		MaxTouchPoints:      0, // desktop
		WebGLVendor:         p.WebGL[0],
		WebGLRenderer:       p.WebGL[1],
	}
}

// matchGeo checks if a timezone roughly matches a geo hint.
func matchGeo(timezone, geo string) bool {
	geoToTimezonePrefix := map[string]string{
		"US": "America/", "VN": "Asia/Ho_Chi_Minh", "JP": "Asia/Tokyo",
		"DE": "Europe/Berlin", "GB": "Europe/London", "EU": "Europe/",
	}
	prefix, ok := geoToTimezonePrefix[geo]
	if !ok {
		return false
	}
	return len(timezone) >= len(prefix) && timezone[:len(prefix)] == prefix
}

// FingerprintInjectionJS returns JavaScript that overrides browser APIs to match the fingerprint.
func FingerprintInjectionJS(fp *Fingerprint) string {
	return fmt.Sprintf(`() => {
		// Override navigator properties
		Object.defineProperty(navigator, 'userAgent', { get: () => %q });
		Object.defineProperty(navigator, 'platform', { get: () => %q });
		Object.defineProperty(navigator, 'vendor', { get: () => %q });
		Object.defineProperty(navigator, 'languages', { get: () => %s });
		Object.defineProperty(navigator, 'language', { get: () => %q });
		Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => %d });
		Object.defineProperty(navigator, 'deviceMemory', { get: () => %d });
		Object.defineProperty(navigator, 'maxTouchPoints', { get: () => %d });

		// Override screen (all dimensions consistent)
		Object.defineProperty(screen, 'width', { get: () => %d });
		Object.defineProperty(screen, 'height', { get: () => %d });
		Object.defineProperty(screen, 'availWidth', { get: () => %d });
		Object.defineProperty(screen, 'availHeight', { get: () => %d });
		Object.defineProperty(screen, 'colorDepth', { get: () => %d });
		Object.defineProperty(screen, 'pixelDepth', { get: () => %d });

		// Window dimensions (consistent with screen)
		Object.defineProperty(window, 'outerWidth', { get: () => %d });
		Object.defineProperty(window, 'outerHeight', { get: () => %d });
		Object.defineProperty(window, 'innerWidth', { get: () => %d });
		Object.defineProperty(window, 'innerHeight', { get: () => %d });

		// Override WebGL fingerprint
		const getParameterOrig = WebGLRenderingContext.prototype.getParameter;
		WebGLRenderingContext.prototype.getParameter = function(param) {
			if (param === 0x9245) return %q; // UNMASKED_VENDOR_WEBGL
			if (param === 0x9246) return %q; // UNMASKED_RENDERER_WEBGL
			return getParameterOrig.call(this, param);
		};
	}`,
		fp.UserAgent, fp.Platform, fp.Vendor,
		languagesJSON(fp.Languages), fp.Languages[0],
		fp.HardwareConcurrency, fp.DeviceMemory, fp.MaxTouchPoints,
		fp.ScreenWidth, fp.ScreenHeight,
		fp.ScreenWidth, fp.ScreenHeight-40,
		fp.ColorDepth, fp.ColorDepth,
		fp.ScreenWidth, fp.ScreenHeight,
		fp.ScreenWidth, fp.ScreenHeight-80,
		fp.WebGLVendor, fp.WebGLRenderer,
	)
}

// FingerprintOnNewDocumentJS returns raw JS (no IIFE wrapper) for use with
// EvalOnNewDocument. Runs before any page JS to set fingerprint overrides.
func FingerprintOnNewDocumentJS(fp *Fingerprint) string {
	return fmt.Sprintf(`
// --- Fingerprint overrides (EvalOnNewDocument) ---
Object.defineProperty(navigator, 'userAgent', { get: () => %q });
Object.defineProperty(navigator, 'platform', { get: () => %q });
Object.defineProperty(navigator, 'vendor', { get: () => %q });
Object.defineProperty(navigator, 'languages', { get: () => %s, configurable: true });
Object.defineProperty(navigator, 'language', { get: () => %q, configurable: true });
Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => %d });
Object.defineProperty(navigator, 'deviceMemory', { get: () => %d });
Object.defineProperty(navigator, 'maxTouchPoints', { get: () => %d });

// Screen dimensions — all properties must be consistent to avoid
// PHANTOM_WINDOW_HEIGHT detection (outerHeight vs screen.height mismatch).
Object.defineProperty(screen, 'width', { get: () => %d });
Object.defineProperty(screen, 'height', { get: () => %d });
Object.defineProperty(screen, 'availWidth', { get: () => %d });
Object.defineProperty(screen, 'availHeight', { get: () => %d });
Object.defineProperty(screen, 'colorDepth', { get: () => %d });
Object.defineProperty(screen, 'pixelDepth', { get: () => %d });

// window.outerWidth/outerHeight must match screen dimensions.
// In a real browser, outer = screen (maximized) or slightly less.
Object.defineProperty(window, 'outerWidth', { get: () => %d });
Object.defineProperty(window, 'outerHeight', { get: () => %d });

// innerWidth/innerHeight = viewport (outerHeight minus chrome UI ~80px).
// CDP sets these via EmulationSetDeviceMetricsOverride, but we override
// to ensure consistency if CDP values differ from our fingerprint.
Object.defineProperty(window, 'innerWidth', { get: () => %d });
Object.defineProperty(window, 'innerHeight', { get: () => %d });

// screenX/screenY — window position on screen (0,0 = maximized)
Object.defineProperty(window, 'screenX', { get: () => 0 });
Object.defineProperty(window, 'screenY', { get: () => 0 });
Object.defineProperty(window, 'screenLeft', { get: () => 0 });
Object.defineProperty(window, 'screenTop', { get: () => 0 });

// WebGL fingerprint
const _fpGetParam = WebGLRenderingContext.prototype.getParameter;
WebGLRenderingContext.prototype.getParameter = function(param) {
	if (param === 0x9245) return %q;
	if (param === 0x9246) return %q;
	return _fpGetParam.call(this, param);
};
`,
		fp.UserAgent, fp.Platform, fp.Vendor,
		languagesJSON(fp.Languages), fp.Languages[0],
		fp.HardwareConcurrency, fp.DeviceMemory, fp.MaxTouchPoints,
		// screen dimensions (all consistent)
		fp.ScreenWidth, fp.ScreenHeight,
		fp.ScreenWidth, fp.ScreenHeight-40, // availHeight = height minus taskbar
		fp.ColorDepth, fp.ColorDepth,
		// window outer = screen
		fp.ScreenWidth, fp.ScreenHeight,
		// window inner = outer minus browser chrome (~80px for toolbar/tabs)
		fp.ScreenWidth, fp.ScreenHeight-80,
		fp.WebGLVendor, fp.WebGLRenderer,
	)
}

func languagesJSON(langs []string) string {
	var result strings.Builder
	result.WriteString("[")
	for i, l := range langs {
		if i > 0 {
			result.WriteString(",")
		}
		result.WriteString(fmt.Sprintf("%q", l))
	}
	return result.String() + "]"
}
