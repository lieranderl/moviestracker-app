package streamlink_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/streamlink"
)

const hash = "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c"

func TestALinkGrantsOneFileUntilItExpires(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	signer := streamlink.NewSigner([]byte("0123456789abcdef0123456789abcdef"), 7*24*time.Hour, streamlink.WithClock(func() time.Time { return now }))

	token := signer.Token(strings.ToUpper(hash), 3)
	grant, err := signer.Verify(token)
	if err != nil || grant.Hash != hash || grant.File != 3 {
		t.Fatalf("Verify(fresh) = %+v, %v; want %s file 3", grant, err, hash)
	}

	now = now.Add(7*24*time.Hour - time.Minute)
	if _, err := signer.Verify(token); err != nil {
		t.Errorf("Verify() just before expiry: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := signer.Verify(token); !errors.Is(err, streamlink.ErrExpired) {
		t.Errorf("Verify() after 7 days = %v, want ErrExpired", err)
	}
}

func TestAChangedLinkIsRefused(t *testing.T) {
	signer := streamlink.NewSigner([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	token := signer.Token(hash, 1)
	parts := strings.Split(token, ".")

	otherFile := strings.Join([]string{parts[0], "2", parts[2], parts[3]}, ".")
	otherHash := strings.Join([]string{strings.Repeat("a", 40), parts[1], parts[2], parts[3]}, ".")
	later := strings.Join([]string{parts[0], parts[1], parts[2] + "9", parts[3]}, ".")
	for name, bad := range map[string]string{
		"another file":    otherFile,
		"another torrent": otherHash,
		"longer lifetime": later,
		"garbage":         "not-a-token",
		"empty":           "",
	} {
		if _, err := signer.Verify(bad); !errors.Is(err, streamlink.ErrInvalid) {
			t.Errorf("%s: Verify() = %v, want ErrInvalid", name, err)
		}
	}
	rotated := streamlink.NewSigner([]byte("fedcba9876543210fedcba9876543210"), time.Hour)
	if _, err := rotated.Verify(token); !errors.Is(err, streamlink.ErrInvalid) {
		t.Errorf("Verify() with a new secret = %v, want ErrInvalid: rotating cancels links", err)
	}
}
