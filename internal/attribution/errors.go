package attribution

import "fmt"

type UnsupportedProjectError struct {
	Shape string
}

func (e *UnsupportedProjectError) Error() string {
	return fmt.Sprintf("unsupported project shape %q; supported: Expo with a static app.json, or one top-level Xcode project with one literal iOS application target. No files were modified", e.Shape)
}

type MissingConfigError struct {
	Root string
}

func (e *MissingConfigError) Error() string {
	return fmt.Sprintf("no %s found in %s; run `attribution init` first", ConfigPath, e.Root)
}

type ConfigExistsError struct {
	Root string
}

func (e *ConfigExistsError) Error() string {
	return fmt.Sprintf("%s already exists in %s; nothing was modified", ConfigPath, e.Root)
}

type MissingExpoPackageError struct{}

func (e *MissingExpoPackageError) Error() string {
	return fmt.Sprintf("required Expo package %s is not declared or its %s entrypoint is not locally resolvable; install it with the project's package manager before planning or applying", AttributionPkg, AttributionEntry)
}

type MissingBundleIdentifierError struct{}

func (e *MissingBundleIdentifierError) Error() string {
	return "the host is missing an explicit bundle identifier; set it to the app's real bundle identifier in expo.ios.bundleIdentifier or every Xcode target PRODUCT_BUNDLE_IDENTIFIER before running attribution init or apply. No files were modified"
}

type DirtyWorkingTreeError struct {
	Paths []string
}

func (e *DirtyWorkingTreeError) Error() string {
	return fmt.Sprintf("working tree has unrelated changes (%s); nothing was modified. Commit or stash them, or re-run with --branch", joinEnglish(e.Paths))
}

type ConfigValidationError struct {
	Problems []string
}

func (e *ConfigValidationError) Error() string {
	return "invalid " + ConfigPath + ": " + joinEnglish(e.Problems)
}

type ProbeValidationError struct {
	Problem string
}

func (e *ProbeValidationError) Error() string {
	return "runtime probe rejected: " + e.Problem + "; no probe artifact was written"
}

func joinEnglish(values []string) string {
	if len(values) == 0 {
		return "unknown error"
	}
	if len(values) == 1 {
		return values[0]
	}
	result := ""
	for i, value := range values {
		if i > 0 {
			result += "; "
		}
		result += value
	}
	return result
}
