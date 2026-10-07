package jacred

import "testing"

func TestAReleaseNameSaysItsSourceCodecAndAudio(t *testing.T) {
	for title, want := range map[string]Format{
		"Человек-паук / Spider-Man (2026) UHD BDRemux 2160p | HDR10 | Dolby Vision | Atmos": {Source: "Remux", Audio: "Atmos"},
		"Spider-Man.2026.2160p.BluRay.x265.10bit.TrueHD.7.1":                                {Source: "BluRay", Codec: "HEVC", TenBit: true, Audio: "7.1"},
		"Человек-паук (2026) BDRip 1080p | D":                                               {Source: "BluRay"},
		"Человек-паук (2026) WEB-DL 1080p от селезень | D":                                  {Source: "WEB-DL"},
		"Человек-паук (2026) WEB-DLRip-AVC от DoMiNo":                                       {Source: "WEB-DLRip", Codec: "AVC"},
		"Spider-Man (2026) [1080p] [WEBRip] [5.1] | The Pirate Bay":                         {Source: "WEBRip", Audio: "5.1"},
		"Spider-Man.2026.1080p.WEBRip.AAC5.1.10bits.x265-Rapta":                             {Source: "WEBRip", Codec: "HEVC", TenBit: true, Audio: "5.1"},
		"Spider-Man.2026.1080p.WEB-DL.DDP5.1.H.264":                                         {Source: "WEB-DL", Codec: "AVC", Audio: "5.1"},
		"Show S02E05 1080p HDTVRip":                                                         {Source: "HDTV"},
		"Человек-паук (2026) DVDRip":                                                        {Source: "DVD"},
		"Человек-паук (2026) CAMRip":                                                        {Source: "CAM", Recording: true},
		"Spider-Man 2026 HDTS 1080p x264":                                                   {Source: "TS", Codec: "AVC", Recording: true},
		"Spider-Man (2026) TeleSync":                                                        {Source: "TS", Recording: true},
		"Человек-паук (2026) [1080p]":                                                       {},
	} {
		if got := (Result{Title: title}).Format(); got != want {
			t.Errorf("Format(%q) = %+v, want %+v", title, got, want)
		}
	}
}
