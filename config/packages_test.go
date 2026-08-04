package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempPackage creates a fake package dir under a temp root.
func writeTempPackage(t *testing.T, root, dir, manifest, skill string) string {
	t.Helper()
	pkgDir := filepath.Join(root, dir)
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(manifest), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if skill != "" {
		if err := os.WriteFile(filepath.Join(pkgDir, "SKILL.md"), []byte(skill), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return pkgDir
}

func TestPackageMetaFromManifest(t *testing.T) {
	dir := writeTempPackage(t, t.TempDir(), "browser", `{
  "name": "browser",
  "description": "CDP Chrome automation",
  "pi": {"description": "Chrome-browser-like CDP automation"}
}`, "")
	name, desc := packageMeta(dir)
	if name != "browser" {
		t.Fatalf("name = %q, want browser", name)
	}
	if desc != "Chrome-browser-like CDP automation" {
		t.Fatalf("desc = %q, want pi.description to win", desc)
	}
}

func TestPackageMetaFallbackToSKILL(t *testing.T) {
	dir := writeTempPackage(t, t.TempDir(), "pkg", "", `---
name: pdf-tools
description: Extract text from PDFs
---
# PDF Tools
`)
	name, desc := packageMeta(dir)
	if name != "pdf-tools" {
		t.Fatalf("name = %q, want pdf-tools", name)
	}
	if desc != "Extract text from PDFs" {
		t.Fatalf("desc = %q, want frontmatter description", desc)
	}
}

func TestPackageMetaMissing(t *testing.T) {
	dir := writeTempPackage(t, t.TempDir(), "empty", "", "")
	name, desc := packageMeta(dir)
	if name != "" || desc != "" {
		t.Fatalf("expected empty meta, got name=%q desc=%q", name, desc)
	}
}

func TestSkillFrontmatter(t *testing.T) {
	name, desc := skillFrontmatter([]byte("---\nname: demo\ndescription: A demo skill\n---\nbody"))
	if name != "demo" || desc != "A demo skill" {
		t.Fatalf("got name=%q desc=%q", name, desc)
	}
	if _, d := skillFrontmatter([]byte("no frontmatter")); d != "" {
		t.Fatal("expected empty for non-frontmatter")
	}
}

func TestPackagesInfoListsProjectPackages(t *testing.T) {
	// Point the project packages root at a temp dir so the test is
	// hermetic and doesn't depend on the repo layout.
	old := packagesRoots
	defer func() { packagesRoots = old }()
	packagesRoots = []string{t.TempDir()}

	writeTempPackage(t, packagesRoots[0], "browser", `{"name":"browser","description":"CDP automation"}`, "")
	writeTempPackage(t, packagesRoots[0], "hidden", "", "") // no desc → skipped

	info := PackagesInfo()
	if !strings.Contains(info, "**browser**") {
		t.Fatalf("PackagesInfo missing browser:\n%s", info)
	}
	if strings.Contains(info, "**hidden**") {
		t.Fatalf("PackagesInfo should skip packages without description:\n%s", info)
	}
	if !strings.Contains(info, "SKILL.md") {
		t.Fatalf("PackagesInfo should point at SKILL.md:\n%s", info)
	}
}
