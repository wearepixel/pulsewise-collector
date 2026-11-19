package main

import (
	"reflect"
	"testing"
)

func TestParseNginxVersion(t *testing.T) {
	if got := parseNginxVersion("nginx version: nginx/1.30.7\n"); got != "1.30.7" {
		t.Fatalf("got %q", got)
	}
	if got := parseNginxVersion("nginx version: nginx/1.18.0 (Ubuntu)"); got != "1.18.0" {
		t.Fatalf("got %q", got)
	}
	if got := parseNginxVersion("command not found"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestParsePHPVersion(t *testing.T) {
	out := "PHP 8.4.3 (fpm-fcgi) (built: Jan 1 2026 00:00:00)\nCopyright (c) The PHP Group\n"
	if got := parsePHPVersion(out); got != "8.4.3" {
		t.Fatalf("got %q", got)
	}
	if got := parsePHPVersion("Warning: something else"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestParseHeldPackagesKeepsOnlyWebStack(t *testing.T) {
	got := parseHeldPackages("nginx\nphp8.4-fpm\nlinux-image-generic\n")
	want := []string{"nginx", "php8.4-fpm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestDeletedExecutableMeansRestartNeeded(t *testing.T) {
	if !isDeletedExecutable("/usr/sbin/nginx (deleted)") {
		t.Fatal("a replaced binary must be flagged")
	}
	if isDeletedExecutable("/usr/sbin/nginx") {
		t.Fatal("a current binary must not be flagged")
	}
}

func TestPHPBranchFromExecutable(t *testing.T) {
	if got := phpBranchFromExecutable("/usr/sbin/php-fpm8.4 (deleted)"); got != "8.4" {
		t.Fatalf("got %q", got)
	}
	if got := phpBranchFromExecutable("/usr/sbin/nginx"); got != "" {
		t.Fatalf("got %q", got)
	}
}
