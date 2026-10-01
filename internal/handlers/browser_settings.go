package handlers

import (
	"context"
	"errors"
	"math"

	"github.com/lieranderl/moviestracker-app/internal/i18n"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// BrowserSettingsSections describes settings editable without a managed engine.
func BrowserSettingsSections() []views.SettingsSection {
	out := make([]views.SettingsSection, 0, len(engineSections)+1)
	for _, sec := range append(append([]settingsSection{}, engineSections...), gstreamerSection) {
		v, _ := BrowserSettingsSection(sec.ID, nil)
		out = append(out, v)
	}
	return out
}

func browserSection(id string) (settingsSection, bool) {
	if id == gstreamerSection.ID {
		return gstreamerSection, true
	}
	return sectionByID(id)
}

// BrowserSettingsSection renders only fields reported by the browser. A nil
// object describes all supported fields, for the browser's relay allowlist.
func BrowserSettingsSection(id string, sets torrserver.Fields) (views.SettingsSection, bool) {
	sec, ok := browserSection(id)
	if !ok {
		return views.SettingsSection{}, false
	}
	filtered := sec
	filtered.Fields = []settingField{}
	for _, f := range sec.Fields {
		if f.Startup {
			continue
		}
		if _, exists := sets[f.Key]; sets != nil && !exists {
			continue
		}
		filtered.Fields = append(filtered.Fields, f)
	}
	v := sectionView(filtered, "")
	v.Cost = reconnectCost
	if id == "gstreamer" {
		v.Cost = gstCost
	}
	for _, f := range filtered.Fields {
		v.Values[f.Key] = shownValue(sets, f)
	}
	return v, true
}

// BrowserSettingsChanges validates a section before a browser writes anything.
// Startup options and unknown fields are refused, never silently accepted.
func BrowserSettingsChanges(ctx context.Context, id string, posted map[string]any) (map[string]any, error) {
	sec, ok := browserSection(id)
	if !ok {
		return nil, errors.New(i18n.T(ctx, "Unknown settings section."))
	}
	changes := make(map[string]any, len(posted))
	for key, value := range posted {
		found := false
		for _, f := range sec.Fields {
			if f.Key != key || f.Startup {
				continue
			}
			found = true
			number, numeric := value.(float64)
			fractional := numeric && math.Trunc(number) != number
			if f.Kind == kindInt && fractional {
				return nil, errors.New(browserFieldProblem(ctx, f, errors.New("must be a whole number")))
			}
			stored, err := f.parse(value)
			if err != nil {
				return nil, errors.New(browserFieldProblem(ctx, f, err))
			}
			changes[key] = stored
		}
		if !found {
			return nil, errors.New(i18n.T(ctx, "unknown setting"))
		}
	}
	return changes, nil
}

func browserFieldProblem(ctx context.Context, f settingField, err error) string {
	st := fieldProblem(ctx, f, err)
	return i18n.Tf(ctx, st.Message, st.Args...)
}
