package browser

import (
	"github.com/go-rod/rod/lib/launcher"
)

// StealthFlags returns Chrome launch flags that reduce automation detection.
// These should be applied to the launcher before Launch().
func StealthFlags(l *launcher.Launcher) {
	// CRITICAL: Rod's default launcher includes "enable-automation" flag which
	// causes Chrome to set navigator.webdriver=true and exposes automation signals.
	// Must delete this before anything else.
	l.Delete("enable-automation")

	// Remove "Chrome is being controlled by automated test software" bar
	l.Set("disable-blink-features", "AutomationControlled")

	// Disable infobars (automation notice)
	l.Set("disable-infobars")

	// Exclude CDP leak signals via Chrome feature flags.
	// AutomationControlled: removes navigator.webdriver at Chrome level.
	// TranslateUI: hides automation-specific UI elements.
	// EnableAutomation: additional automation detection surface.
	l.Set("disable-features", "AutomationControlled,TranslateUI,EnableAutomation")

	// Exclude "cdc_" prefix variables that automation tools inject.
	// Chrome DevTools Protocol leaves $cdc_ prefixed variables in the DOM.
	l.Set("enable-features", "NetworkService,NetworkServiceInProcess")

	// Standard fingerprint-normalizing flags
	l.Set("disable-background-networking")
	l.Set("disable-client-side-phishing-detection")
	l.Set("disable-default-apps")
	l.Set("disable-hang-monitor")
	l.Set("disable-popup-blocking")
	l.Set("disable-prompt-on-repost")
	l.Set("disable-sync")
	l.Set("metrics-recording-only")
	l.Set("no-first-run")
	l.Set("password-store", "basic")
	l.Set("use-mock-keychain")
	l.Set("export-tagged-pdf")

	// Disable automation extension that leaks signals
	l.Set("disable-extensions")
	l.Set("disable-component-extensions-with-background-pages")

	// Prevent WebRTC from leaking real IP behind proxy
	l.Set("enforce-webrtc-ip-permission-check")
	l.Set("disable-webrtc-hw-decoding")
	l.Set("disable-webrtc-hw-encoding")

	// Window size — headless default is 800x600 which is a dead giveaway
	l.Set("window-size", "1920,1080")

	// Force English locale — prevents leaking real system locale
	l.Set("lang", "en-US")
	l.Set("accept-lang", "en-US,en")
}

// StealthJS is JavaScript to inject on every new page to remove automation traces.
// Call page.Eval(StealthJS) after page creation.
// NOTE: For new pages, prefer stealthOnNewDocumentJS with EvalOnNewDocument
// which runs BEFORE any page JS (critical for defeating navigator.webdriver checks).
const StealthJS = `() => {
	// Remove navigator.webdriver from prototype + instance
	delete Object.getPrototypeOf(navigator).webdriver;
	Object.defineProperty(Navigator.prototype, 'webdriver', {
		get: () => false,
		configurable: true,
		enumerable: true,
	});
	Object.defineProperty(navigator, 'webdriver', {
		get: () => false,
		configurable: true,
		enumerable: true,
	});

	// Fix chrome.runtime to look real
	if (!window.chrome) {
		window.chrome = {};
	}
	if (!window.chrome.runtime) {
		window.chrome.runtime = {
			connect: function() {},
			sendMessage: function() {},
		};
	}

	// Fix permissions query for notifications
	if (navigator.permissions) {
		const originalQuery = navigator.permissions.query;
		navigator.permissions.query = (parameters) => {
			if (parameters.name === 'notifications') {
				return Promise.resolve({ state: 'prompt', onchange: null });
			}
			return originalQuery.call(navigator.permissions, parameters).catch(() =>
				Promise.resolve({ state: 'prompt', onchange: null })
			);
		};
	}

	// Fix languages (should match fingerprint if set)
	Object.defineProperty(navigator, 'languages', {
		get: () => ['en-US', 'en'],
		configurable: true,
	});
}`

// stealthOnNewDocumentJS is raw JS (no IIFE wrapper) for use with
// Page.addScriptToEvaluateOnNewDocument via rod's EvalOnNewDocument.
// This runs BEFORE any page JavaScript, which is critical for defeating
// Google/Cloudflare bot detection that reads navigator.webdriver early.
const stealthOnNewDocumentJS = `
// --- navigator.webdriver: DELETE entirely ---
// bot.sannysoft.com checks 'webdriver' in navigator (presence, not value).
// Chrome re-adds the property during navigation via Object.defineProperty on
// Navigator.prototype, so we intercept defineProperty to block it.
delete Navigator.prototype.webdriver;
const _origDefProp = Object.defineProperty;
Object.defineProperty = function(obj, prop, desc) {
	if ((obj === Navigator.prototype || obj === navigator) && prop === 'webdriver') {
		return obj; // silently block Chrome from re-adding webdriver
	}
	return _origDefProp.call(this, obj, prop, desc);
};

// --- chrome object ---
if (!window.chrome) window.chrome = {};
if (!window.chrome.runtime) {
	window.chrome.runtime = { connect: function(){}, sendMessage: function(){} };
}
if (!window.chrome.csi) window.chrome.csi = function() { return {}; };
if (!window.chrome.loadTimes) window.chrome.loadTimes = function() { return {}; };

// --- Permissions API ---
// Headless Chrome returns "denied" for notifications — real Chrome returns "prompt".
if (window.navigator && window.navigator.permissions) {
	const origQuery = window.navigator.permissions.query;
	window.navigator.permissions.query = (params) => {
		if (params.name === 'notifications') {
			return Promise.resolve({ state: 'prompt', onchange: null });
		}
		return origQuery.call(navigator.permissions, params).catch(() =>
			Promise.resolve({ state: 'prompt', onchange: null })
		);
	};
}

// --- Plugins (Chrome has at least 3) ---
// Use Object.create(PluginArray.prototype) when available so instanceof check passes.
// All own properties set via _origDefProp to override native getters on the prototype
// (which throw Illegal invocation on non-native objects).
try {
(function() {
	const pluginData = [
		{ name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer', description: 'Portable Document Format', length: 1 },
		{ name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', description: '', length: 1 },
		{ name: 'Native Client', filename: 'internal-nacl-plugin', description: '', length: 0 },
	];
	// Each plugin must toString() as "[object Plugin]" — sannysoft checks this.
	const hasPlugin = typeof Plugin !== 'undefined' && Plugin.prototype;
	for (let i = 0; i < pluginData.length; i++) {
		const p = hasPlugin ? Object.create(Plugin.prototype) : {};
		for (const k of Object.keys(pluginData[i])) {
			_origDefProp.call(Object, p, k, { value: pluginData[i][k], enumerable: true });
		}
		if (!hasPlugin) _origDefProp.call(Object, p, Symbol.toStringTag, { value: 'Plugin' });
		pluginData[i] = p;
	}
	const hasProto = typeof PluginArray !== 'undefined' && PluginArray.prototype;
	const fakePlugins = hasProto ? Object.create(PluginArray.prototype) : {};
	for (let i = 0; i < pluginData.length; i++) {
		_origDefProp.call(Object, fakePlugins, String(i), { value: pluginData[i], enumerable: true });
	}
	_origDefProp.call(Object, fakePlugins, 'length', { value: pluginData.length, enumerable: true });
	_origDefProp.call(Object, fakePlugins, 'item', { value: function(i) { return this[i] || null; } });
	_origDefProp.call(Object, fakePlugins, 'namedItem', { value: function(n) { for (let i = 0; i < this.length; i++) if (this[i].name === n) return this[i]; return null; } });
	_origDefProp.call(Object, fakePlugins, 'refresh', { value: function() {} });
	_origDefProp.call(Object, fakePlugins, Symbol.iterator, { value: function*() { for (let i = 0; i < this.length; i++) yield this[i]; } });
	if (!hasProto) _origDefProp.call(Object, fakePlugins, Symbol.toStringTag, { value: 'PluginArray' });
	_origDefProp.call(Object, Navigator.prototype, 'plugins', {
		get: () => fakePlugins,
		configurable: true,
	});
})();
} catch(e) { /* fallback gracefully */ }

// --- Languages (override on prototype to beat Chrome's native getter) ---
_origDefProp.call(Object, Navigator.prototype, 'languages', {
	get: () => ['en-US', 'en'],
	configurable: true,
});
_origDefProp.call(Object, Navigator.prototype, 'language', {
	get: () => 'en-US',
	configurable: true,
});

// --- Function.prototype.toString: hide CDP sourceURL leak ---
const _origToString = Function.prototype.toString;
Function.prototype.toString = function() {
	if (this === Function.prototype.toString) return 'function toString() { [native code] }';
	return _origToString.call(this).replace(/\/\/# sourceURL=.*$/gm, '');
};

// --- navigator.connection (missing in headless) ---
if (!navigator.connection) {
	_origDefProp.call(Object, navigator, 'connection', {
		get: () => ({
			effectiveType: '4g',
			rtt: 50,
			downlink: 10,
			saveData: false,
		}),
	});
}

// --- iframe contentWindow.chrome detection ---
const _origCreateElement = document.createElement;
document.createElement = function(...args) {
	const el = _origCreateElement.apply(this, args);
	if (args[0] && args[0].toLowerCase() === 'iframe') {
		el.addEventListener('load', () => {
			try {
				if (el.contentWindow && !el.contentWindow.chrome) {
					el.contentWindow.chrome = window.chrome;
				}
			} catch(e) { /* cross-origin */ }
		});
	}
	return el;
};

// --- CDP Runtime.enable detection bypass ---
const _origCaptureStackTrace = Error.captureStackTrace;
if (_origCaptureStackTrace) {
	Error.captureStackTrace = function(targetObject, constructorOpt) {
		_origCaptureStackTrace.call(Error, targetObject, constructorOpt);
		const origDescriptor = _origDefProp === Object.defineProperty ? null :
			Object.getOwnPropertyDescriptor(targetObject, 'stack');
		if (origDescriptor && origDescriptor.get) {
			const origGet = origDescriptor.get;
			_origDefProp.call(Object, targetObject, 'stack', {
				get: function() { return origGet.call(this); },
				set: origDescriptor.set,
				configurable: true,
			});
		}
	};
}

// --- Web Worker fingerprint consistency ---
const _origWorker = window.Worker;
if (_origWorker) {
	window.Worker = function(scriptURL, options) {
		return new _origWorker(scriptURL, options);
	};
	window.Worker.prototype = _origWorker.prototype;
	_origDefProp.call(Object, window.Worker, 'name', { value: 'Worker' });
}

// --- navigator.mimeTypes (headless may have empty) ---
try {
if (!navigator.mimeTypes || navigator.mimeTypes.length === 0) {
	const fakeMimeTypes = {
		0: { type: 'application/pdf', suffixes: 'pdf', description: 'Portable Document Format' },
		1: { type: 'text/pdf', suffixes: 'pdf', description: '' },
		length: 2,
		item: function(i) { return this[i] || null; },
		namedItem: function(n) { for (let i = 0; i < this.length; i++) if (this[i].type === n) return this[i]; return null; },
		[Symbol.iterator]: function*() { for (let i = 0; i < this.length; i++) yield this[i]; },
		[Symbol.toStringTag]: 'MimeTypeArray',
	};
	_origDefProp.call(Object, Navigator.prototype, 'mimeTypes', {
		get: () => fakeMimeTypes,
		configurable: true,
	});
}
} catch(e) { /* fallback gracefully */ }

// --- Broken image dimensions fix ---
// In headless Chrome, broken images return 0x0. Real Chrome returns 16x16.
const _origImage = window.Image;
window.Image = function(w, h) {
	const img = new _origImage(w, h);
	img.addEventListener('error', () => {
		if (img.naturalWidth === 0 && img.naturalHeight === 0) {
			_origDefProp.call(Object, img, 'naturalWidth', { get: () => 16 });
			_origDefProp.call(Object, img, 'naturalHeight', { get: () => 16 });
		}
	});
	return img;
};
window.Image.prototype = _origImage.prototype;
_origDefProp.call(Object, window.Image, 'name', { value: 'Image' });

// --- WebGL fallback: fake context when real one unavailable ---
// Headless containers often lack GPU. Sannysoft checks getParameter(UNMASKED_VENDOR/RENDERER).
// We intercept getContext to return a minimal fake when real WebGL is null.
const _origGetContext = HTMLCanvasElement.prototype.getContext;
HTMLCanvasElement.prototype.getContext = function(type, attrs) {
	const ctx = _origGetContext.call(this, type, attrs);
	if (ctx) return ctx;
	if (type === 'webgl' || type === 'experimental-webgl' || type === 'webgl2') {
		// Return a minimal fake that satisfies vendor/renderer checks.
		// getParameter and getSupportedExtensions are all sannysoft needs.
		if (!this._fakeGL) {
			this._fakeGL = {
				getParameter: function(p) {
					// UNMASKED_VENDOR_WEBGL / UNMASKED_RENDERER_WEBGL
					if (p === 0x9245) return 'Google Inc. (NVIDIA)';
					if (p === 0x9246) return 'ANGLE (NVIDIA, NVIDIA GeForce GTX 1080 Direct3D11 vs_5_0 ps_5_0)';
					if (p === 0x1F01) return 'WebKit WebGL'; // VERSION
					if (p === 0x1F00) return 'WebKit';       // VENDOR
					if (p === 0x8B8C) return 'WebGL GLSL ES 1.0'; // SHADING_LANGUAGE_VERSION
					return null;
				},
				getSupportedExtensions: function() { return ['WEBGL_debug_renderer_info']; },
				getExtension: function(name) {
					if (name === 'WEBGL_debug_renderer_info') return { UNMASKED_VENDOR_WEBGL: 0x9245, UNMASKED_RENDERER_WEBGL: 0x9246 };
					return null;
				},
			};
		}
		return this._fakeGL;
	}
	return ctx;
};
`
