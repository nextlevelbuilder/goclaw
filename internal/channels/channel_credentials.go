package channels

// nonSecretCredentialKeys lists credential keys that are safe to expose
// unmasked in API responses, per channel type. These are identifiers or
// operator-registered URLs, never secrets. Everything else is masked as
// "***" by the HTTP/WS instance responders.
var nonSecretCredentialKeys = map[string]map[string]bool{
	TypeZaloOA: {
		"app_id":       true, // public Zalo application identifier used for developer-console links
		"oa_id":        true, // public Official Account ID (bound during consent)
		"redirect_uri": true, // operator-registered callback URL
	},
}

// IsNonSecretCredentialKey reports whether key may be returned unmasked for
// the given channel type.
func IsNonSecretCredentialKey(channelType, key string) bool {
	return nonSecretCredentialKeys[channelType][key]
}
