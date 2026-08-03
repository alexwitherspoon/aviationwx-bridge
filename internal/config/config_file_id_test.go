package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateConfigFileID(t *testing.T) {
	if err := validateConfigFileID("tower-north", "camera"); err != nil {
		t.Fatalf("valid id: %v", err)
	}
	cases := []string{
		"",
		"../x",
		"bad/id",
		`bad\id`,
		"dots.not",
		"has space",
		"..",
		"/etc/passwd",
	}
	for _, id := range cases {
		if err := validateConfigFileID(id, "camera"); err == nil {
			t.Fatalf("validateConfigFileID(%q) expected error", id)
		}
	}
}

func TestContainedConfigPath_staysUnderSubdir(t *testing.T) {
	base := t.TempDir()
	path, err := containedConfigPath(base, "stations", "wll-1", "station")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "wll-1.json" {
		t.Fatalf("path = %q", path)
	}
	wantPrefix := filepath.Join(base, "stations") + string(filepath.Separator)
	absWant, err := filepath.Abs(wantPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, absWant) {
		t.Fatalf("path %q not under %q", path, absWant)
	}
	for _, id := range []string{"../etc/passwd", "a/b", ".."} {
		if _, err := containedConfigPath(base, "stations", id, "station"); err == nil {
			t.Fatalf("expected rejection for id %q", id)
		}
	}
}

func TestValidateStationID(t *testing.T) {
	if err := ValidateStationID("davis-north"); err != nil {
		t.Fatalf("valid id: %v", err)
	}
	for _, id := range []string{"", "../x", "bad/id", "dots.not"} {
		if err := ValidateStationID(id); err == nil {
			t.Fatalf("ValidateStationID(%q) expected error", id)
		}
	}
}

func TestStationConfigPath_staysUnderStationsDir(t *testing.T) {
	base := t.TempDir()
	path, err := StationConfigPath(base, "st-a")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "st-a.json" {
		t.Fatalf("path = %q", path)
	}
	if _, err := StationConfigPath(base, "../etc/passwd"); err == nil {
		t.Fatal("expected traversal id to fail")
	}
}

func TestDeleteStation_rejectsInvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteStation("../escape"); err == nil {
		t.Fatal("expected invalid id error")
	}
}

func TestSlugStationIDFromName(t *testing.T) {
	got := SlugStationIDFromName("Scappoose Davis")
	if got != "scappoose-davis" {
		t.Fatalf("got %q", got)
	}
}
