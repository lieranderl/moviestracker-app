package jacred

import (
	"encoding/json"
	"errors"
	"io"
	"time"
)

func decodeJackett(body io.Reader) ([]rawResult, error) {
	var page struct {
		Results *[]struct {
			Tracker     string
			Details     string
			Title       string
			Size        int64
			PublishDate string
			Seeders     int
			Peers       int
			MagnetURI   string `json:"MagnetUri"`
			Info        rawResult
		}
	}
	if err := json.NewDecoder(body).Decode(&page); err != nil {
		return nil, err
	}
	if page.Results == nil {
		return nil, errors.New("missing Jackett results")
	}
	raw := make([]rawResult, 0, len(*page.Results))
	for _, r := range *page.Results {
		v := r.Info
		v.Tracker, v.URL, v.Title = r.Tracker, r.Details, r.Title
		v.Size, v.Seeders, v.Peers, v.Magnet = r.Size, r.Seeders, r.Peers, r.MagnetURI
		v.CreateTime = r.PublishDate
		if t, err := time.Parse("2006-01-02T15:04:05", r.PublishDate); err == nil {
			v.CreateTime = t.Format(time.RFC3339)
		}
		raw = append(raw, v)
	}
	return raw, nil
}
