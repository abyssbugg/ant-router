// Package codexversion validates supported Codex CLI versions.
package codexversion

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	TestedMajor = 0
	TestedMinor = 153
)

type Version struct{ Major, Minor, Patch uint64 }

func (version Version) String() string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}

func Parse(output string) (Version, error) {
	value := strings.TrimSpace(output)
	if !strings.HasPrefix(value, "codex-cli ") {
		return Version{}, errors.New("invalid Codex version output")
	}
	parts := strings.Split(strings.TrimPrefix(value, "codex-cli "), ".")
	if len(parts) != 3 {
		return Version{}, errors.New("invalid Codex version output")
	}
	values := [3]uint64{}
	for index, part := range parts {
		if part == "" {
			return Version{}, errors.New("invalid Codex version output")
		}
		parsed, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return Version{}, errors.New("invalid Codex version output")
		}
		values[index] = parsed
	}
	return Version{Major: values[0], Minor: values[1], Patch: values[2]}, nil
}

func Check(output string, allowUntested bool) (Version, error) {
	version, err := Parse(output)
	if err != nil {
		return Version{}, err
	}
	if version.Major < TestedMajor || (version.Major == TestedMajor && version.Minor < TestedMinor) {
		return Version{}, errors.New("unsupported Codex version")
	}
	if version.Major > TestedMajor || version.Minor > TestedMinor {
		if !allowUntested {
			return Version{}, errors.New("untested Codex version")
		}
	}
	return version, nil
}
