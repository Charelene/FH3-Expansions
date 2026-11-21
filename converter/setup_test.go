package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, dir, identity, display string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "media"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := `<?xml version="1.0" encoding="utf-8"?><Package><Identity Name="` + identity + `" Version="1.0.0.0"/>` +
		`<Properties><DisplayName>` + display + `</DisplayName></Properties></Package>`
	if err := os.WriteFile(filepath.Join(dir, "AppxManifest.xml"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The expansions kept inside the base game's folder, each with a nested copy of
// itself (a "src" folder) and a converted copy beside them.
func TestFindDumpPackagesNested(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, baseGameName, "Forza Horizon 3")
	hw := filepath.Join(root, "Microsoft.ForzaHorizon3HotWheels")
	bm := filepath.Join(root, "Microsoft.ForzaHorizon3BlizzardMountain")
	writeManifest(t, hw, hotWheelsName, "Forza Horizon 3 Hot Wheels")
	writeManifest(t, bm, blizzardName, "Forza Horizon 3 Blizzard Mountain")
	writeManifest(t, filepath.Join(hw, "src"), hotWheelsName, "Forza Horizon 3 Hot Wheels")
	writeManifest(t, filepath.Join(bm, "src"), blizzardName, "Forza Horizon 3 Blizzard Mountain")
	writeManifest(t, filepath.Join(root, "converted", "HotWheels"), hotWheelsName, "HOT WHEELS")

	b, h, z, err := findDumpPackages(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if b != root || h != hw || z != bm {
		t.Fatalf("found base %s, Hot Wheels %s, Blizzard Mountain %s", b, h, z)
	}
}

// Two separate copies of one expansion are still refused.
func TestFindDumpPackagesTwoCopies(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "base"), baseGameName, "Forza Horizon 3")
	writeManifest(t, filepath.Join(root, "a", "HotWheels"), hotWheelsName, "Forza Horizon 3 Hot Wheels")
	writeManifest(t, filepath.Join(root, "b", "HotWheels"), hotWheelsName, "Forza Horizon 3 Hot Wheels")
	writeManifest(t, filepath.Join(root, "BlizzardMountain"), blizzardName, "Forza Horizon 3 Blizzard Mountain")
	if _, _, _, err := findDumpPackages(root, ""); err == nil {
		t.Fatal("two copies of Hot Wheels were accepted")
	}
}
