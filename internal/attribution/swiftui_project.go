package attribution

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const maxPBXProjectBytes = 4 * 1024 * 1024

type SwiftUIProject struct {
	XcodeProject       string
	ProjectFile        string
	TargetID           string
	TargetName         string
	InfoPlistPath      string
	GeneratesInfoPlist bool
	objects            map[string]string
	targetBlock        string
}

type SwiftUIIntegration struct {
	PackageLinked  bool
	PackageProblem string
	SourceTargeted bool
	SourceProblem  string
}

var pbxObjectStartPattern = regexp.MustCompile(`(?m)^\s*([A-Fa-f0-9]{24})\s*(?:/\*[^\n]*?\*/\s*)?=\s*\{`)

func DiscoverSwiftUI(root string) (Project, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return Project{}, &UnsupportedProjectError{Shape: "project path is not a directory"}
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return Project{}, fmt.Errorf("enumerate Xcode projects: %w", err)
	}
	var projects []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".xcodeproj") {
			projects = append(projects, entry.Name())
		}
	}
	sort.Strings(projects)
	if len(projects) != 1 {
		return Project{}, &UnsupportedProjectError{Shape: fmt.Sprintf("expected exactly one top-level .xcodeproj, found %d", len(projects))}
	}
	projectRelative := projects[0]
	projectPath := filepath.Join(absolute, projectRelative)
	projectInfo, err := os.Lstat(projectPath)
	if err != nil || !projectInfo.IsDir() || projectInfo.Mode()&os.ModeSymlink != 0 {
		return Project{}, &UnsupportedProjectError{Shape: projectRelative + " must be a non-symlink directory"}
	}
	projectFileRelative := filepath.ToSlash(filepath.Join(projectRelative, "project.pbxproj"))
	if err := validateSafeTarget(absolute, projectFileRelative); err != nil {
		return Project{}, &UnsupportedProjectError{Shape: err.Error()}
	}
	projectFile := filepath.Join(projectPath, "project.pbxproj")
	fileInfo, err := os.Lstat(projectFile)
	if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Mode()&os.ModeSymlink != 0 || fileInfo.Size() <= 0 || fileInfo.Size() > maxPBXProjectBytes {
		return Project{}, &UnsupportedProjectError{Shape: projectFileRelative + " must be a bounded regular non-symlink file"}
	}
	raw, err := os.ReadFile(projectFile)
	if err != nil {
		return Project{}, fmt.Errorf("read %s: %w", projectFileRelative, err)
	}
	objects, err := parsePBXObjects(string(raw))
	if err != nil {
		return Project{}, &UnsupportedProjectError{Shape: "invalid project.pbxproj: " + err.Error()}
	}
	type targetCandidate struct {
		id    string
		block string
	}
	var targets []targetCandidate
	for id, block := range objects {
		if pbxScalar(block, "isa") != "PBXNativeTarget" {
			continue
		}
		if pbxScalar(block, "productType") == "com.apple.product-type.application" {
			targets = append(targets, targetCandidate{id: id, block: block})
		}
	}
	if len(targets) != 1 {
		return Project{}, &UnsupportedProjectError{Shape: fmt.Sprintf("expected exactly one iOS application target, found %d", len(targets))}
	}
	target := targets[0]
	targetName := pbxScalar(target.block, "name")
	if targetName == "" {
		return Project{}, &UnsupportedProjectError{Shape: "application target has no resolvable name"}
	}
	configurationListID := pbxReference(pbxScalarRaw(target.block, "buildConfigurationList"))
	configurationList := objects[configurationListID]
	if configurationList == "" {
		return Project{}, &UnsupportedProjectError{Shape: "application target build configuration list is missing"}
	}
	configurationIDs := pbxArrayReferences(configurationList, "buildConfigurations")
	if len(configurationIDs) == 0 {
		return Project{}, &UnsupportedProjectError{Shape: "application target has no build configurations"}
	}
	var bundleID, infoPlistPath, infoPlistMode string
	for _, configurationID := range configurationIDs {
		configuration := objects[configurationID]
		if configuration == "" || pbxScalar(configuration, "isa") != "XCBuildConfiguration" {
			return Project{}, &UnsupportedProjectError{Shape: "application target references an invalid build configuration"}
		}
		currentBundle := pbxScalar(configuration, "PRODUCT_BUNDLE_IDENTIFIER")
		if currentBundle == "" || containsBuildVariable(currentBundle) {
			return Project{}, &UnsupportedProjectError{Shape: "PRODUCT_BUNDLE_IDENTIFIER must be an explicit literal in every target build configuration"}
		}
		if bundleID == "" {
			bundleID = currentBundle
		} else if bundleID != currentBundle {
			return Project{}, &UnsupportedProjectError{Shape: "PRODUCT_BUNDLE_IDENTIFIER differs across target build configurations"}
		}
		currentInfo := pbxScalar(configuration, "INFOPLIST_FILE")
		currentGenerated := strings.EqualFold(pbxScalar(configuration, "GENERATE_INFOPLIST_FILE"), "YES")
		if currentInfo != "" {
			if currentGenerated {
				return Project{}, &UnsupportedProjectError{Shape: "INFOPLIST_FILE and GENERATE_INFOPLIST_FILE = YES cannot both be active in a target build configuration"}
			}
			if containsBuildVariable(currentInfo) || filepath.IsAbs(filepath.FromSlash(currentInfo)) {
				return Project{}, &UnsupportedProjectError{Shape: "INFOPLIST_FILE must be an explicit project-relative literal without build variables"}
			}
			currentInfo = filepath.ToSlash(filepath.Clean(filepath.FromSlash(currentInfo)))
			if currentInfo == ".." || strings.HasPrefix(currentInfo, "../") {
				return Project{}, &UnsupportedProjectError{Shape: "INFOPLIST_FILE escapes the project root"}
			}
			if infoPlistPath == "" {
				infoPlistPath = currentInfo
			} else if infoPlistPath != currentInfo {
				return Project{}, &UnsupportedProjectError{Shape: "INFOPLIST_FILE differs across target build configurations"}
			}
			if infoPlistMode == "" {
				infoPlistMode = "explicit"
			} else if infoPlistMode != "explicit" {
				return Project{}, &UnsupportedProjectError{Shape: "Info.plist mode differs across target build configurations"}
			}
		} else if !currentGenerated {
			return Project{}, &UnsupportedProjectError{Shape: "each target build configuration must declare a literal INFOPLIST_FILE or GENERATE_INFOPLIST_FILE = YES"}
		} else if infoPlistMode == "" {
			infoPlistMode = "generated"
		} else if infoPlistMode != "generated" {
			return Project{}, &UnsupportedProjectError{Shape: "Info.plist mode differs across target build configurations"}
		}
	}
	if infoPlistPath != "" {
		if err := validateSafeTarget(absolute, infoPlistPath); err != nil {
			return Project{}, &UnsupportedProjectError{Shape: "unsafe INFOPLIST_FILE: " + err.Error()}
		}
	}

	swift := &SwiftUIProject{
		XcodeProject:       projectRelative,
		ProjectFile:        projectFileRelative,
		TargetID:           target.id,
		TargetName:         targetName,
		InfoPlistPath:      infoPlistPath,
		GeneratesInfoPlist: infoPlistMode == "generated",
		objects:            objects,
		targetBlock:        target.block,
	}
	return Project{
		Root:           absolute,
		Host:           "swiftui",
		PackageManager: "swiftpm",
		Dependencies:   map[string]string{},
		BundleID:       bundleID,
		SwiftUI:        swift,
	}, nil
}

func containsBuildVariable(value string) bool {
	return strings.Contains(value, "$(") || strings.Contains(value, "${")
}

func parsePBXObjects(raw string) (map[string]string, error) {
	matches := pbxObjectStartPattern.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		return nil, errors.New("no object records found")
	}
	objects := make(map[string]string, len(matches))
	for _, match := range matches {
		id := raw[match[2]:match[3]]
		opening := strings.LastIndex(raw[match[0]:match[1]], "{") + match[0]
		closing, err := matchingPBXBrace(raw, opening)
		if err != nil {
			return nil, err
		}
		if _, duplicate := objects[id]; duplicate {
			return nil, fmt.Errorf("duplicate object id %s", id)
		}
		objects[id] = raw[opening : closing+1]
	}
	return objects, nil
}

func matchingPBXBrace(raw string, opening int) (int, error) {
	depth := 0
	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false
	for i := opening; i < len(raw); i++ {
		if inLineComment {
			if raw[i] == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if i+1 < len(raw) && raw[i] == '*' && raw[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inString {
			if escaped {
				escaped = false
			} else if raw[i] == '\\' {
				escaped = true
			} else if raw[i] == '"' {
				inString = false
			}
			continue
		}
		if i+1 < len(raw) && raw[i] == '/' && raw[i+1] == '/' {
			inLineComment = true
			i++
			continue
		}
		if i+1 < len(raw) && raw[i] == '/' && raw[i+1] == '*' {
			inBlockComment = true
			i++
			continue
		}
		switch raw[i] {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, errors.New("unterminated object record")
}

func pbxScalarRaw(block, key string) string {
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*(.+?);\s*$`)
	match := pattern.FindStringSubmatch(block)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func pbxScalar(block, key string) string {
	value := pbxScalarRaw(block, key)
	if index := strings.Index(value, " /*"); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if decoded, err := strconv.Unquote(value); err == nil {
			return decoded
		}
	}
	return value
}

func pbxReference(raw string) string {
	match := regexp.MustCompile(`^[A-Fa-f0-9]{24}`).FindString(strings.TrimSpace(raw))
	return match
}

func pbxArrayReferences(block, key string) []string {
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=\s*\(`)
	location := pattern.FindStringIndex(block)
	if location == nil {
		return nil
	}
	rest := block[location[1]:]
	end := strings.Index(rest, ");")
	if end < 0 {
		return nil
	}
	return regexp.MustCompile(`[A-Fa-f0-9]{24}`).FindAllString(rest[:end], -1)
}

func inspectSwiftUIIntegration(project Project) SwiftUIIntegration {
	if project.SwiftUI == nil {
		return SwiftUIIntegration{PackageProblem: "not a SwiftUI project", SourceProblem: "not a SwiftUI project"}
	}
	swift := project.SwiftUI
	integration := SwiftUIIntegration{}
	productIDs := pbxArrayReferences(swift.targetBlock, "packageProductDependencies")
	frameworkProductRefs := targetFrameworkProductReferences(swift)
	var matchingProducts int
	for _, productID := range productIDs {
		product := swift.objects[productID]
		if pbxScalar(product, "isa") != "XCSwiftPackageProductDependency" || pbxScalar(product, "productName") != "AttributionCore" {
			continue
		}
		packageID := pbxReference(pbxScalarRaw(product, "package"))
		if packageID == "" || !isOfficialAttributionPackage(project.Root, swift.objects[packageID]) {
			continue
		}
		linkedCount := 0
		for _, frameworkProductID := range frameworkProductRefs {
			if frameworkProductID == productID {
				linkedCount++
			}
		}
		if linkedCount == 1 {
			matchingProducts++
		}
	}
	if matchingProducts == 1 {
		integration.PackageLinked = true
	} else if matchingProducts == 0 {
		integration.PackageProblem = "target does not link the AttributionCore product from the official AttributionKit Swift package"
	} else {
		integration.PackageProblem = "target links AttributionCore more than once"
	}

	sourceRefs := targetSourceFileReferences(swift)
	var matchingSources int
	for _, fileRef := range sourceRefs {
		resolved, ok := resolvePBXFileReference(swift, fileRef)
		if ok && resolved == SwiftSourcePath {
			matchingSources++
		}
	}
	if matchingSources == 1 {
		integration.SourceTargeted = true
	} else if matchingSources == 0 {
		integration.SourceProblem = "generated Swift source is not a member of the application target's Sources build phase"
	} else {
		integration.SourceProblem = "generated Swift source is compiled more than once"
	}
	return integration
}

func targetFrameworkProductReferences(swift *SwiftUIProject) []string {
	var refs []string
	for _, phaseID := range pbxArrayReferences(swift.targetBlock, "buildPhases") {
		phase := swift.objects[phaseID]
		if pbxScalar(phase, "isa") != "PBXFrameworksBuildPhase" {
			continue
		}
		for _, buildFileID := range pbxArrayReferences(phase, "files") {
			buildFile := swift.objects[buildFileID]
			if pbxScalar(buildFile, "isa") != "PBXBuildFile" {
				continue
			}
			if productRef := pbxReference(pbxScalarRaw(buildFile, "productRef")); productRef != "" {
				refs = append(refs, productRef)
			}
		}
	}
	return refs
}

func isOfficialAttributionPackage(root, block string) bool {
	switch pbxScalar(block, "isa") {
	case "XCRemoteSwiftPackageReference":
		url := strings.TrimSuffix(strings.TrimSuffix(pbxScalar(block, "repositoryURL"), "/"), ".git")
		return strings.EqualFold(url, AttributionRepo)
	case "XCLocalSwiftPackageReference":
		relative := pbxScalar(block, "relativePath")
		if relative == "" || containsBuildVariable(relative) || filepath.IsAbs(filepath.FromSlash(relative)) {
			return false
		}
		packageFile := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative), "Package.swift"))
		info, err := os.Lstat(packageFile)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 1024*1024 {
			return false
		}
		raw, err := os.ReadFile(packageFile)
		if err != nil {
			return false
		}
		text := string(raw)
		return strings.Contains(text, `name: "AttributionKit"`) && strings.Contains(text, `.library(name: "AttributionCore"`)
	default:
		return false
	}
}

func targetSourceFileReferences(swift *SwiftUIProject) []string {
	var refs []string
	for _, phaseID := range pbxArrayReferences(swift.targetBlock, "buildPhases") {
		phase := swift.objects[phaseID]
		if pbxScalar(phase, "isa") != "PBXSourcesBuildPhase" {
			continue
		}
		for _, buildFileID := range pbxArrayReferences(phase, "files") {
			buildFile := swift.objects[buildFileID]
			if pbxScalar(buildFile, "isa") != "PBXBuildFile" {
				continue
			}
			if fileRef := pbxReference(pbxScalarRaw(buildFile, "fileRef")); fileRef != "" {
				refs = append(refs, fileRef)
			}
		}
	}
	return refs
}

func resolvePBXFileReference(swift *SwiftUIProject, fileRefID string) (string, bool) {
	fileRef := swift.objects[fileRefID]
	if pbxScalar(fileRef, "isa") != "PBXFileReference" {
		return "", false
	}
	path := pbxScalar(fileRef, "path")
	if path == "" {
		path = pbxScalar(fileRef, "name")
	}
	if path == "" || containsBuildVariable(path) {
		return "", false
	}
	sourceTree := pbxScalar(fileRef, "sourceTree")
	if sourceTree == "SOURCE_ROOT" {
		return cleanPBXRelative(path)
	}
	if sourceTree != "<group>" && sourceTree != "" {
		return "", false
	}
	parents := make(map[string]string)
	for groupID, block := range swift.objects {
		isa := pbxScalar(block, "isa")
		if isa != "PBXGroup" && isa != "PBXFileSystemSynchronizedRootGroup" {
			continue
		}
		for _, child := range pbxArrayReferences(block, "children") {
			if existing, found := parents[child]; found && existing != groupID {
				return "", false
			}
			parents[child] = groupID
		}
	}
	base, ok := resolvePBXGroupPath(swift, parents[fileRefID], parents, map[string]bool{})
	if !ok {
		return "", false
	}
	return cleanPBXRelative(filepath.ToSlash(filepath.Join(filepath.FromSlash(base), filepath.FromSlash(path))))
}

func resolvePBXGroupPath(swift *SwiftUIProject, groupID string, parents map[string]string, seen map[string]bool) (string, bool) {
	if groupID == "" {
		return "", true
	}
	if seen[groupID] {
		return "", false
	}
	seen[groupID] = true
	group := swift.objects[groupID]
	isa := pbxScalar(group, "isa")
	if isa != "PBXGroup" && isa != "PBXFileSystemSynchronizedRootGroup" {
		return "", false
	}
	path := pbxScalar(group, "path")
	sourceTree := pbxScalar(group, "sourceTree")
	if sourceTree == "SOURCE_ROOT" {
		return cleanPBXRelative(path)
	}
	if sourceTree != "<group>" && sourceTree != "" {
		return "", false
	}
	parent, ok := resolvePBXGroupPath(swift, parents[groupID], parents, seen)
	if !ok {
		return "", false
	}
	return cleanPBXRelative(filepath.ToSlash(filepath.Join(filepath.FromSlash(parent), filepath.FromSlash(path))))
}

func cleanPBXRelative(path string) (string, bool) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." {
		clean = ""
	}
	if filepath.IsAbs(filepath.FromSlash(clean)) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}
