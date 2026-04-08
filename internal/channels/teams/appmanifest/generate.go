package appmanifest

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
)

//go:embed color.png
var defaultColorIcon []byte

//go:embed outline.png
var defaultOutlineIcon []byte

const (
	schemaURL       = "https://developer.microsoft.com/json-schemas/teams/v1.19/MicrosoftTeams.schema.json"
	manifestVersion = "1.19"
	maxNameShort    = 30
	maxNameFull     = 100
	maxDescShort    = 80
	maxDescFull     = 4000
	maxIconBytes    = 500 * 1024 // 500KB
)

var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// Options configures the Teams app manifest generation.
type Options struct {
	BotID           string   // required — Azure App Registration ID
	Name            string   // required — short name (<=30)
	FullName        string   // optional — falls back to Name
	Description     string   // optional — default "AI assistant powered by GoClaw"
	FullDescription string   // optional — default = Description
	Developer       string   // optional — default "GoClaw"
	WebsiteURL      string   // optional — default "https://goclaw.dev"
	PrivacyURL      string   // optional — default "https://goclaw.dev/privacy"
	TermsURL        string   // optional — default "https://goclaw.dev/terms"
	Version         string   // optional — default "1.0.0"
	AccentColor     string   // optional — default "#4B53BC"
	Scopes          []string // optional — default ["personal","team","groupChat"]
	ColorIcon       []byte   // optional — custom 192x192 PNG
	OutlineIcon     []byte   // optional — custom 32x32 PNG
}

// Manifest represents the Teams app manifest.json structure.
type Manifest struct {
	Schema          string     `json:"$schema"`
	ManifestVersion string     `json:"manifestVersion"`
	ID              string     `json:"id"`
	Version         string     `json:"version"`
	Name            NameField  `json:"name"`
	Description     DescField  `json:"description"`
	Developer       DevField   `json:"developer"`
	Icons           IconsField `json:"icons"`
	AccentColor     string     `json:"accentColor"`
	Bots            []BotEntry `json:"bots"`
	Permissions     []string   `json:"permissions"`
	ValidDomains    []string   `json:"validDomains"`
}

// NameField represents the name section of manifest.
type NameField struct {
	Short string `json:"short"`
	Full  string `json:"full"`
}

// DescField represents the description section of manifest.
type DescField struct {
	Short string `json:"short"`
	Full  string `json:"full"`
}

// DevField represents the developer section of manifest.
type DevField struct {
	Name       string `json:"name"`
	WebsiteURL string `json:"websiteUrl"`
	PrivacyURL string `json:"privacyUrl"`
	TermsURL   string `json:"termsOfUseUrl"`
}

// BotEntry represents a bot entry in manifest.
type BotEntry struct {
	BotID  string     `json:"botId"`
	Scopes []string   `json:"scopes"`
	Cmds   []struct{} `json:"commandLists,omitempty"`
}

// IconsField represents the icons section of manifest.
type IconsField struct {
	Color   string `json:"color"`
	Outline string `json:"outline"`
}

// GenerateZIP creates a Teams app package ZIP containing manifest.json and icon files.
func GenerateZIP(opts Options) ([]byte, error) {
	if err := validate(&opts); err != nil {
		return nil, err
	}
	applyDefaults(&opts)
	truncateFields(&opts)

	m := Manifest{
		Schema:          schemaURL,
		ManifestVersion: manifestVersion,
		ID:              opts.BotID,
		Version:         opts.Version,
		Name:            NameField{Short: opts.Name, Full: opts.FullName},
		Description:     DescField{Short: opts.Description, Full: opts.FullDescription},
		Developer: DevField{
			Name:       opts.Developer,
			WebsiteURL: opts.WebsiteURL,
			PrivacyURL: opts.PrivacyURL,
			TermsURL:   opts.TermsURL,
		},
		Icons:       IconsField{Color: "color.png", Outline: "outline.png"},
		AccentColor: opts.AccentColor,
		Bots: []BotEntry{{
			BotID:  opts.BotID,
			Scopes: opts.Scopes,
		}},
		Permissions:  []string{"identity", "messageTeamMembers"},
		ValidDomains: []string{},
	}

	manifestJSON, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling manifest: %w", err)
	}

	colorIcon := defaultColorIcon
	if len(opts.ColorIcon) > 0 {
		colorIcon = opts.ColorIcon
	}
	outlineIcon := defaultOutlineIcon
	if len(opts.OutlineIcon) > 0 {
		outlineIcon = opts.OutlineIcon
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, f := range []struct {
		name string
		data []byte
	}{
		{"manifest.json", manifestJSON},
		{"color.png", colorIcon},
		{"outline.png", outlineIcon},
	} {
		w, err := zw.Create(f.name)
		if err != nil {
			return nil, fmt.Errorf("creating zip entry %s: %w", f.name, err)
		}
		if _, err := w.Write(f.data); err != nil {
			return nil, fmt.Errorf("writing zip entry %s: %w", f.name, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("closing zip: %w", err)
	}
	return buf.Bytes(), nil
}

func validate(opts *Options) error {
	if opts.BotID == "" {
		return errors.New("bot_id is required: Teams channel not configured")
	}
	if opts.Name == "" {
		return errors.New("name is required")
	}
	if len(opts.ColorIcon) > 0 {
		if err := validateIcon(opts.ColorIcon, "color"); err != nil {
			return err
		}
	}
	if len(opts.OutlineIcon) > 0 {
		if err := validateIcon(opts.OutlineIcon, "outline"); err != nil {
			return err
		}
	}
	return nil
}

func validateIcon(data []byte, label string) error {
	if len(data) < len(pngMagic) || !bytes.HasPrefix(data, pngMagic) {
		return fmt.Errorf("%s icon: invalid PNG format", label)
	}
	if len(data) > maxIconBytes {
		return fmt.Errorf("%s icon: exceeds 500KB limit (%d bytes)", label, len(data))
	}
	return nil
}

func applyDefaults(opts *Options) {
	if opts.FullName == "" {
		opts.FullName = opts.Name
	}
	if opts.Description == "" {
		opts.Description = "AI assistant powered by GoClaw"
	}
	if opts.FullDescription == "" {
		opts.FullDescription = opts.Description
	}
	if opts.Developer == "" {
		opts.Developer = "GoClaw"
	}
	if opts.WebsiteURL == "" {
		opts.WebsiteURL = "https://goclaw.dev"
	}
	if opts.PrivacyURL == "" {
		opts.PrivacyURL = "https://goclaw.dev/privacy"
	}
	if opts.TermsURL == "" {
		opts.TermsURL = "https://goclaw.dev/terms"
	}
	if opts.AccentColor == "" {
		opts.AccentColor = "#4B53BC"
	}
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}
	if len(opts.Scopes) == 0 {
		opts.Scopes = []string{"personal", "team", "groupChat"}
	}
}

// truncateRunes truncates a string to max runes (not bytes) to preserve Unicode integrity.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max])
	}
	return s
}

func truncateFields(opts *Options) {
	opts.Name = truncateRunes(opts.Name, maxNameShort)
	opts.FullName = truncateRunes(opts.FullName, maxNameFull)
	opts.Description = truncateRunes(opts.Description, maxDescShort)
	opts.FullDescription = truncateRunes(opts.FullDescription, maxDescFull)
}
