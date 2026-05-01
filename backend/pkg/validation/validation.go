package validation

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	domainPrefixRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,28}[a-z0-9])?$`)
	resourceNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)
	imageTagRe     = regexp.MustCompile(`^[a-z0-9]+([._/-][a-z0-9]+)*(:[A-Za-z0-9_.-]{1,128})?$`)
	sizeRe         = regexp.MustCompile(`^[1-9][0-9]*[kKmMgG]$`)
	numberRe       = regexp.MustCompile(`^[1-9][0-9]*$`)
)

func DomainPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if !domainPrefixRe.MatchString(prefix) {
		return fmt.Errorf("domain prefix must be a DNS label: lowercase letters, digits and hyphen only")
	}
	return nil
}

func ReservedDomainPrefix(prefix string, reserved []string) bool {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return false
	}
	for _, item := range reserved {
		if prefix == strings.ToLower(strings.TrimSpace(item)) {
			return true
		}
	}
	return false
}

func ResourceName(name string) error {
	if !resourceNameRe.MatchString(name) {
		return fmt.Errorf("name must start with a letter or digit and contain only letters, digits, underscore, dot or hyphen")
	}
	return nil
}

func ProjectName(name string) error {
	return ResourceName(name)
}

func ImageTag(tag string) error {
	if !imageTagRe.MatchString(tag) || strings.Contains(tag, "..") || strings.Contains(tag, "//") {
		return fmt.Errorf("image tag must be a Docker Hub-style image reference without a registry host")
	}
	return nil
}

func MountPath(path string) error {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "\x00") {
		return fmt.Errorf("mount path must be an absolute container path")
	}
	clean := strings.TrimRight(path, "/")
	switch {
	case clean == "":
		return fmt.Errorf("mounting a volume to / is not allowed")
	case clean == "/proc" || strings.HasPrefix(clean, "/proc/"):
		return fmt.Errorf("mounting a volume to /proc is not allowed")
	case clean == "/sys" || strings.HasPrefix(clean, "/sys/"):
		return fmt.Errorf("mounting a volume to /sys is not allowed")
	case clean == "/dev" || strings.HasPrefix(clean, "/dev/"):
		return fmt.Errorf("mounting a volume to /dev is not allowed")
	case clean == "/var/run/docker.sock":
		return fmt.Errorf("mounting over docker.sock is not allowed")
	}
	return nil
}

func DockerSize(value string) error {
	if !sizeRe.MatchString(value) {
		return fmt.Errorf("size must be a positive Docker size like 10m or 1G")
	}
	return nil
}

func PositiveNumberString(value string) error {
	if !numberRe.MatchString(value) {
		return fmt.Errorf("value must be a positive integer")
	}
	return nil
}
