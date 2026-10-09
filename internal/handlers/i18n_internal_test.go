package handlers

import (
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

// Settings pages translate their fields, and handlers the fixed messages
// and errors they show; each has a Russian translation.
func TestSettingsAndMessagesHaveRussianTranslations(t *testing.T) {
	var texts []string
	for _, sec := range append(engineSections, gstreamerSection) {
		texts = append(texts, sec.Title, sec.Intro)
		for _, f := range sec.Fields {
			texts = append(texts, f.Label, f.Help, f.Unit)
			for _, c := range f.Choices {
				texts = append(texts, c.Label)
			}
			if f.Check != nil {
				if err := f.Check("not valid\x00"); err != nil {
					texts = append(texts, err.Error())
				}
				if err := f.Check("relative/not-valid"); err != nil {
					texts = append(texts, err.Error())
				}
			}
			for _, bad := range []any{struct{}{}, "x"} {
				if _, err := f.parse(bad); err != nil {
					if _, ranged := err.(outOfRange); !ranged {
						texts = append(texts, err.Error())
					}
				}
			}
		}
	}
	texts = append(texts, reconnectCost, startupOnly, engineAsleep, gstCost, httpsCost, gstNotBuilt, noToneMapper, saveFailed,
		"Certificate uploaded. TorrServer serves it now.", "TorrServer serves the certificate from these files now.",
		"TorrServer serves its self-signed certificate now.", "New self-signed certificate made. Browsers ask to accept it again.",
		"must be between %d and %d %s", "contains an invalid character", sharedViewer, "A browser")
	for _, err := range []error{auth.ErrInvalidCredentials, auth.ErrInvalidAccount, auth.ErrAccountExists,
		auth.ErrInvalidRole, auth.ErrNoAccount, auth.ErrLastAdmin} {
		texts = append(texts, err.Error())
	}
	for _, text := range texts {
		if text != "" && !i18n.Has(i18n.Russian, text) {
			t.Errorf("no Russian for %q", text)
		}
	}
}
