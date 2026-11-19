package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const softwareInterval = time.Hour

type SoftwareInfo struct {
	Nginx          *NginxSoftware `json:"nginx,omitempty"`
	PHP            []PHPSoftware  `json:"php,omitempty"`
	OS             *OSInfo        `json:"os,omitempty"`
	HeldPackages   []string       `json:"held_packages,omitempty"`
	RebootRequired bool           `json:"reboot_required"`
}

type OSInfo struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
	LTS       bool   `json:"lts"`
}

type NginxSoftware struct {
	Version      string `json:"version"`
	Package      string `json:"package,omitempty"`
	NeedsRestart bool   `json:"needs_restart"`
}

type PHPSoftware struct {
	Branch       string `json:"branch"`
	Version      string `json:"version"`
	Package      string `json:"package,omitempty"`
	NeedsRestart bool   `json:"needs_restart"`
}

var (
	nginxVersionPattern = regexp.MustCompile(`nginx/(\d+\.\d+\.\d+)`)
	phpVersionPattern   = regexp.MustCompile(`^PHP (\d+\.\d+\.\d+)`)
	phpFPMBinaryPattern = regexp.MustCompile(`php-fpm(\d+\.\d+)`)

	softwareMu       sync.Mutex
	softwareLastSent time.Time
)

func softwareDue() bool {
	softwareMu.Lock()
	defer softwareMu.Unlock()
	return time.Since(softwareLastSent) >= softwareInterval
}

func markSoftwareSent() {
	softwareMu.Lock()
	defer softwareMu.Unlock()
	softwareLastSent = time.Now()
}

func collectSoftware() *SoftwareInfo {
	if runtime.GOOS != "linux" {
		return nil
	}

	masters := runningMasterExecutables()
	info := &SoftwareInfo{}

	if version := nginxInstalledVersion(); version != "" {
		info.Nginx = &NginxSoftware{
			Version:      version,
			Package:      debianPackageVersion("nginx"),
			NeedsRestart: masters.nginxDeleted,
		}
	}

	for _, branch := range phpBranchesInstalled() {
		version := phpInstalledVersion(branch)
		if version == "" {
			continue
		}
		info.PHP = append(info.PHP, PHPSoftware{
			Branch:       branch,
			Version:      version,
			Package:      debianPackageVersion("php" + branch + "-fpm"),
			NeedsRestart: masters.phpDeleted[branch],
		})
	}

	info.OS = installedOS()
	info.HeldPackages = heldWebPackages()
	_, err := os.Stat("/var/run/reboot-required")
	info.RebootRequired = err == nil

	if info.Nginx == nil && len(info.PHP) == 0 {
		return nil
	}

	return info
}

func parseNginxVersion(output string) string {
	match := nginxVersionPattern.FindStringSubmatch(output)
	if match == nil {
		return ""
	}
	return match[1]
}

func parsePHPVersion(output string) string {
	match := phpVersionPattern.FindStringSubmatch(strings.TrimSpace(output))
	if match == nil {
		return ""
	}
	return match[1]
}

func parseHeldPackages(output string) []string {
	var held []string
	for _, line := range strings.Split(output, "\n") {
		name := strings.TrimSpace(line)
		if strings.HasPrefix(name, "nginx") || strings.HasPrefix(name, "php") {
			held = append(held, name)
		}
	}
	return held
}

func isDeletedExecutable(link string) bool {
	return strings.HasSuffix(link, " (deleted)")
}

func phpBranchFromExecutable(link string) string {
	match := phpFPMBinaryPattern.FindStringSubmatch(link)
	if match == nil {
		return ""
	}
	return match[1]
}

func nginxInstalledVersion() string {
	path, err := exec.LookPath("nginx")
	if err != nil {
		path = "/usr/sbin/nginx"
	}
	// nginx writes its version to stderr.
	out, err := exec.Command(path, "-v").CombinedOutput()
	if err != nil {
		return ""
	}
	return parseNginxVersion(string(out))
}

func phpBranchesInstalled() []string {
	paths, _ := filepath.Glob("/usr/sbin/php-fpm[0-9]*.[0-9]*")
	var branches []string
	for _, path := range paths {
		if branch := phpBranchFromExecutable(path); branch != "" {
			branches = append(branches, branch)
		}
	}
	return branches
}

func phpInstalledVersion(branch string) string {
	out, err := exec.Command("/usr/sbin/php-fpm"+branch, "-n", "-v").Output()
	if err != nil {
		return ""
	}
	return parsePHPVersion(string(out))
}

func debianPackageVersion(pkg string) string {
	out, err := exec.Command("dpkg-query", "-W", "-f=${Version}", pkg).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func heldWebPackages() []string {
	out, err := exec.Command("apt-mark", "showhold").Output()
	if err != nil {
		return nil
	}
	return parseHeldPackages(string(out))
}

type masterExecutables struct {
	nginxDeleted bool
	phpDeleted   map[string]bool
}

// A package upgrade replaces the binary on disk but the running master keeps
// the old one mapped: /proc/<pid>/exe then ends in " (deleted)" until restart.
func runningMasterExecutables() masterExecutables {
	result := masterExecutables{phpDeleted: map[string]bool{}}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] < '0' || entry.Name()[0] > '9' {
			continue
		}

		cmdline, err := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
		if err != nil {
			continue
		}
		title := string(bytes.ReplaceAll(cmdline, []byte{0}, []byte{' '}))

		isNginx := strings.HasPrefix(title, "nginx: master process")
		isPHP := strings.HasPrefix(title, "php-fpm: master process")
		if !isNginx && !isPHP {
			continue
		}

		link, err := os.Readlink("/proc/" + entry.Name() + "/exe")
		if err != nil {
			continue
		}

		if isNginx && isDeletedExecutable(link) {
			result.nginxDeleted = true
		}
		if isPHP && isDeletedExecutable(link) {
			if branch := phpBranchFromExecutable(link); branch != "" {
				result.phpDeleted[branch] = true
			}
		}
	}

	return result
}

func installedOS() *OSInfo {
	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return nil
	}
	return parseOSRelease(string(content))
}

func parseOSRelease(content string) *OSInfo {
	values := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			values[key] = strings.Trim(value, `"'`)
		}
	}

	if values["ID"] == "" || values["VERSION_ID"] == "" {
		return nil
	}

	return &OSInfo{
		ID:        values["ID"],
		VersionID: values["VERSION_ID"],
		LTS:       strings.Contains(values["VERSION"], "LTS"),
	}
}
