package main

import (
	"os"
	"path/filepath"
	"strings"
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

// A Store build's manifest (an optional package, no content package declared)
// gets the short name and <mx:ContentPackage>true, stays valid XML, and the
// original it was hard-linked to is not touched.
func TestSetExpansionNameStoreManifest(t *testing.T) {
	src := t.TempDir()
	m := "<?xml version=\"1.0\" encoding=\"utf-8\"?>\r\n" +
		`<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10" xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10" xmlns:uap3="http://schemas.microsoft.com/appx/manifest/uap/windows10/3" IgnorableNamespaces="uap uap3">` + "\r\n" +
		`  <Identity Name="Microsoft.ForzaHorizon3HotWheels" Version="1.0.63.2" />` + "\r\n" +
		"  <Properties>\r\n    <DisplayName>Forza Horizon 3 Hot Wheels</DisplayName>\r\n    <Logo>Logo.png</Logo>\r\n  </Properties>\r\n" +
		`  <Dependencies><uap3:MainPackageDependency Name="Microsoft.OpusPG" /></Dependencies>` + "\r\n" +
		`  <Applications><Application Id="x"><uap:VisualElements AppListEntry="none" DisplayName="Forza Horizon 3 Hot Wheels" /></Application></Applications>` + "\r\n" +
		"</Package>\r\n"
	orig := filepath.Join(src, "AppxManifest.xml")
	if err := os.WriteFile(orig, []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := os.Link(orig, filepath.Join(dst, "AppxManifest.xml")); err != nil {
		t.Fatal(err)
	}
	if err := setExpansionName(dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dst, "AppxManifest.xml"))
	s := string(b)
	if !declaresContentPackage.MatchString(s) {
		t.Error("no content package declared:\n" + s)
	}
	if !strings.Contains(s, `xmlns:mx="`+mxNamespace+`"`) {
		t.Error("mx namespace not declared")
	}
	if displayName(dst) != "HOT WHEELS" || !strings.Contains(s, `DisplayName="HOT WHEELS"`) {
		t.Error("short name not set:\n" + s)
	}
	if id, err := packageIdentity(dst); err != nil || id != hotWheelsName {
		t.Errorf("manifest no longer reads: %q %v", id, err)
	}
	if o, _ := os.ReadFile(orig); string(o) != m {
		t.Error("the original manifest was changed")
	}
	// a second run changes nothing
	if err := setExpansionName(dst); err != nil {
		t.Fatal(err)
	}
	if b2, _ := os.ReadFile(filepath.Join(dst, "AppxManifest.xml")); string(b2) != s {
		t.Error("second run changed the manifest")
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
