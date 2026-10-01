package handlers

import (
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine"
)

// EngineOptions turns the saved startup settings into engine options.
func EngineOptions(st config.EngineStartup) engine.Options {
	return engine.Options{
		ProxyURL:    st.ProxyURL,
		ProxyMode:   st.ProxyMode,
		PublicIPv4:  st.PublicIPv4,
		PublicIPv6:  st.PublicIPv6,
		MaxSize:     st.MaxSize,
		TorrentsDir: st.TorrentsDir,
		HTTPS:       st.HTTPS,
		Reachable:   st.Reachable,
	}
}
