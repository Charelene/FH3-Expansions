package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Both forms the expansions were published in: an Xbox content package, and the
// Store's Windows optional package that depends on the main game.
func TestReadPackageManifestForms(t *testing.T) {
	forms := map[string]string{
		"xbox content package": `<?xml version="1.0" encoding="utf-8"?>
<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10" xmlns:mx="http://schemas.microsoft.com/appx/2013/xbox/manifest">
  <Identity Name="Microsoft.ForzaHorizon3HotWheels" Version="1.0.1.70" />
  <Properties><DisplayName>Forza Horizon 3 Hot Wheels</DisplayName><mx:ContentPackage>true</mx:ContentPackage></Properties>
  <Extensions><mx:PackageExtension Category="xbox.contentpackage"><mx:ContentPackage><mx:AllowedProduct Id="ef520a86-ebbe-4a01-8384-7b0eef90ab50" /></mx:ContentPackage></mx:PackageExtension></Extensions>
</Package>`,
		"windows optional package": `<?xml version="1.0" encoding="utf-8"?>
<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10" xmlns:uap3="http://schemas.microsoft.com/appx/manifest/uap/windows10/3">
  <Identity Name="Microsoft.ForzaHorizon3HotWheels" Version="1.0.63.2" />
  <Properties><DisplayName>Forza Horizon 3 Hot Wheels</DisplayName></Properties>
  <Dependencies><uap3:MainPackageDependency Name="Microsoft.OpusPG" /></Dependencies>
</Package>`,
	}
	for name, m := range forms {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "AppxManifest.xml"), []byte(m), 0o644); err != nil {
			t.Fatal(err)
		}
		p, err := readPackage(packages[1], dir)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if p.version == "" || p.manifestHash == "" || p.treeDigest == "" {
			t.Errorf("%s: incomplete package %+v", name, p)
		}
	}

	// a package that names neither is refused
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AppxManifest.xml"), []byte(`<Package><Identity Name="Microsoft.ForzaHorizon3HotWheels" Version="1.0.0.0" /><Properties><DisplayName>X</DisplayName></Properties></Package>`), 0o644)
	if _, err := readPackage(packages[1], dir); err == nil {
		t.Error("a package not tied to FH3 was accepted")
	}
}
