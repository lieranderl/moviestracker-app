package views_test

import (
	"context"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestTheCertificateUploadTabReadsAsUploadingFilesInRussian(t *testing.T) {
	st := &torrserver.SSLStatus{Enabled: true, Port: "8091", Cert: torrserver.CertInfo{Source: torrserver.CertSelfSigned}}
	var out strings.Builder
	ctx := i18n.WithLang(context.Background(), i18n.Russian)
	if err := views.WebCertificate(st, false, time.Now()).Render(ctx, &out); err != nil {
		t.Fatal(err)
	}
	page := html.UnescapeString(out.String())
	// "Отдача" is seeding to peers (the torrent settings' Upload), not
	// sending a certificate.
	if strings.Contains(page, `aria-label="Отдача"`) || !strings.Contains(page, `aria-label="Загрузка файлов"`) {
		t.Errorf("the upload tab is not labelled Загрузка файлов:\n%s", page)
	}
}
