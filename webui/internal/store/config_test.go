package store

import (
	"path/filepath"
	"testing"
)

func TestLoadAndSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	in := &Config{
		Devices: []DeviceConfig{
			{ID: "dev1", IP: "192.168.1.50"},
			{ID: "dev2", IP: "192.168.1.51"},
		},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out.Devices) != 2 || out.Devices[0].IP != "192.168.1.50" {
		t.Fatalf("unexpected loaded config: %+v", out.Devices)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
