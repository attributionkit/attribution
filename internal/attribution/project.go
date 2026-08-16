package attribution

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Project struct {
	Root           string
	Host           string
	PackageManager string
	Dependencies   map[string]string
	PackageJSON    map[string]any
	AppJSON        map[string]any
	BundleID       string
	SwiftUI        *SwiftUIProject
}

func DiscoverProject(root string) (Project, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return Project{}, &UnsupportedProjectError{Shape: "project path is not a directory"}
	}
	var xcodeProjects []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".xcodeproj") {
			xcodeProjects = append(xcodeProjects, entry.Name())
		}
	}
	_, packageErr := os.Lstat(filepath.Join(absolute, "package.json"))
	_, appErr := os.Lstat(filepath.Join(absolute, "app.json"))
	hasExpoShape := packageErr == nil || appErr == nil
	if hasExpoShape && len(xcodeProjects) > 0 {
		return Project{}, &UnsupportedProjectError{Shape: "ambiguous Expo and Xcode project roots; pass the exact host project directory"}
	}
	if len(xcodeProjects) > 0 {
		return DiscoverSwiftUI(absolute)
	}
	return DiscoverExpo(absolute)
}

func DiscoverExpo(root string) (Project, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return Project{}, &UnsupportedProjectError{Shape: "project path is not a directory"}
	}
	for _, path := range []string{"package.json", "app.json"} {
		if err := validateSafeTarget(absolute, path); err != nil {
			return Project{}, &UnsupportedProjectError{Shape: err.Error()}
		}
	}

	packagePath := filepath.Join(absolute, "package.json")
	packageRaw, err := os.ReadFile(packagePath)
	if errors.Is(err, os.ErrNotExist) {
		return Project{}, &UnsupportedProjectError{Shape: "missing package.json"}
	}
	if err != nil {
		return Project{}, fmt.Errorf("read package.json: %w", err)
	}
	packageJSON, err := decodeJSONObject(packageRaw)
	if err != nil {
		return Project{}, &UnsupportedProjectError{Shape: "invalid package.json: " + err.Error()}
	}
	dependencies, err := packageDependencies(packageJSON)
	if err != nil {
		return Project{}, &UnsupportedProjectError{Shape: err.Error()}
	}
	if _, ok := dependencies["expo"]; !ok {
		return Project{}, &UnsupportedProjectError{Shape: "bare React Native or non-Expo package"}
	}

	appPath := filepath.Join(absolute, "app.json")
	appRaw, err := os.ReadFile(appPath)
	if errors.Is(err, os.ErrNotExist) {
		for _, dynamic := range []string{"app.config.js", "app.config.cjs", "app.config.mjs", "app.config.ts"} {
			if _, statErr := os.Stat(filepath.Join(absolute, dynamic)); statErr == nil {
				return Project{}, &UnsupportedProjectError{Shape: "dynamic Expo config (" + dynamic + ")"}
			}
		}
		return Project{}, &UnsupportedProjectError{Shape: "Expo project missing app.json"}
	}
	if err != nil {
		return Project{}, fmt.Errorf("read app.json: %w", err)
	}
	appJSON, err := decodeJSONObject(appRaw)
	if err != nil {
		return Project{}, &UnsupportedProjectError{Shape: "invalid app.json: " + err.Error()}
	}
	expo, ok := appJSON["expo"].(map[string]any)
	if !ok {
		return Project{}, &UnsupportedProjectError{Shape: "app.json does not contain an Expo object"}
	}
	if err := validatePluginShape(expo["plugins"]); err != nil {
		return Project{}, &UnsupportedProjectError{Shape: err.Error()}
	}

	bundleID := ""
	if ios, ok := expo["ios"].(map[string]any); ok {
		if value, ok := ios["bundleIdentifier"].(string); ok {
			bundleID = value
		}
	}
	manager, err := detectPackageManager(absolute, packageJSON)
	if err != nil {
		return Project{}, &UnsupportedProjectError{Shape: err.Error()}
	}
	return Project{
		Root:           absolute,
		Host:           "expo",
		PackageManager: manager,
		Dependencies:   dependencies,
		PackageJSON:    packageJSON,
		AppJSON:        appJSON,
		BundleID:       bundleID,
	}, nil
}

func decodeJSONObject(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("root must be an object")
	}
	return object, nil
}

func packageDependencies(packageJSON map[string]any) (map[string]string, error) {
	dependencies := make(map[string]string)
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		value, exists := packageJSON[key]
		if !exists {
			continue
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("package.json %s must be an object", key)
		}
		for name, version := range object {
			versionString, ok := version.(string)
			if !ok || strings.TrimSpace(versionString) == "" {
				return nil, fmt.Errorf("package.json %s.%s must be a non-empty string", key, name)
			}
			dependencies[name] = versionString
		}
	}
	return dependencies, nil
}

func validatePluginShape(value any) error {
	if value == nil {
		return nil
	}
	plugins, ok := value.([]any)
	if !ok {
		return errors.New("app.json expo.plugins must be an array")
	}
	for i, plugin := range plugins {
		switch item := plugin.(type) {
		case string:
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("app.json expo.plugins[%d] must not be empty", i)
			}
		case []any:
			if len(item) < 1 || len(item) > 2 {
				return fmt.Errorf("app.json expo.plugins[%d] tuple must contain a plugin name and optional options", i)
			}
			name, ok := item[0].(string)
			if !ok || strings.TrimSpace(name) == "" {
				return fmt.Errorf("app.json expo.plugins[%d] tuple must start with a plugin name", i)
			}
			if len(item) == 2 {
				if _, ok := item[1].(map[string]any); !ok && item[1] != nil {
					return fmt.Errorf("app.json expo.plugins[%d] options must be an object", i)
				}
			}
		default:
			return fmt.Errorf("app.json expo.plugins[%d] has an unsupported shape", i)
		}
	}
	return nil
}

func detectPackageManager(root string, packageJSON map[string]any) (string, error) {
	lockfiles := map[string][]string{
		"bun":  {"bun.lock", "bun.lockb"},
		"pnpm": {"pnpm-lock.yaml"},
		"yarn": {"yarn.lock"},
		"npm":  {"package-lock.json", "npm-shrinkwrap.json"},
	}
	detected := make([]string, 0, 1)
	for manager, files := range lockfiles {
		for _, file := range files {
			if _, err := os.Stat(filepath.Join(root, file)); err == nil {
				detected = append(detected, manager)
				break
			}
		}
	}
	sort.Strings(detected)
	if len(detected) > 1 {
		return "", fmt.Errorf("multiple package-manager lockfiles detected: %s", strings.Join(detected, ", "))
	}

	declared := ""
	if raw, exists := packageJSON["packageManager"]; exists {
		value, ok := raw.(string)
		if !ok {
			return "", errors.New("package.json packageManager must be a string")
		}
		declared = strings.SplitN(value, "@", 2)[0]
		if declared != "npm" && declared != "yarn" && declared != "pnpm" && declared != "bun" {
			return "", fmt.Errorf("unknown package manager %q", declared)
		}
	}
	if len(detected) == 1 {
		if declared != "" && declared != detected[0] {
			return "", fmt.Errorf("packageManager declares %s but the project has a %s lockfile", declared, detected[0])
		}
		return detected[0], nil
	}
	if declared != "" {
		return declared, nil
	}
	// npm is the conservative Expo default when neither Corepack metadata nor
	// a lockfile exists. It is derived, not persisted into desired state.
	return "npm", nil
}

func installedManagers(project Project) []conversionManager {
	var managers []conversionManager
	for _, manager := range knownManagers {
		if _, installed := project.Dependencies[manager.Package]; installed {
			managers = append(managers, manager)
		}
	}
	sort.Slice(managers, func(i, j int) bool { return managers[i].Package < managers[j].Package })
	return managers
}

func attributionPackageInstalled(project Project) bool {
	if _, declared := project.Dependencies[AttributionPkg]; !declared {
		return false
	}
	if findInstalledNodeModuleFile(project.Root, filepath.Join("@attributionkit", "expo", "app.plugin.js")) {
		return true
	}
	// Yarn Plug'n'Play intentionally has no node_modules tree. Ask its local
	// runtime to resolve the exact public entrypoint; this performs no install
	// and no network request.
	if project.PackageManager == "yarn" {
		command := exec.Command("yarn", "node", "-e", "require.resolve("+strconv.Quote(AttributionEntry)+")")
		command.Dir = project.Root
		return command.Run() == nil
	}
	return false
}

func managerPackageInstalled(project Project, manager conversionManager) bool {
	if _, declared := project.Dependencies[manager.Package]; !declared {
		return false
	}
	if findInstalledNodeModuleFile(project.Root, filepath.Join(filepath.FromSlash(manager.Package), "package.json")) {
		return true
	}
	if project.PackageManager == "yarn" {
		command := exec.Command("yarn", "node", "-e", "require.resolve("+strconv.Quote(manager.Package)+")")
		command.Dir = project.Root
		return command.Run() == nil
	}
	return false
}

func findInstalledNodeModuleFile(start, relative string) bool {
	current := start
	for {
		candidate := filepath.Join(current, "node_modules", relative)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}

func metaIsAuthority(config Config) bool {
	for _, manager := range knownManagers {
		if manager.Package == "react-native-fbsdk-next" {
			return managerMatchesOwner(manager, config.ConversionAuthority.Owner)
		}
	}
	return false
}

func shouldDisableMetaConversion(config Config) bool {
	return config.Providers.Meta != nil && !metaIsAuthority(config)
}

func pluginRegistered(appJSON map[string]any) bool {
	expo, ok := appJSON["expo"].(map[string]any)
	if !ok {
		return false
	}
	plugins, ok := expo["plugins"].([]any)
	if !ok {
		return false
	}
	wanted := "./" + PluginPath
	exact := 0
	for index, plugin := range plugins {
		name, ok := pluginRegistrationName(plugin)
		if !ok || !isAttributionPluginRegistration(name) {
			continue
		}
		if name != wanted || index != len(plugins)-1 {
			return false
		}
		if _, tuple := plugin.([]any); tuple {
			return false
		}
		exact++
	}
	return exact == 1
}

func legacyPluginRegistered(appJSON map[string]any) bool {
	expo, ok := appJSON["expo"].(map[string]any)
	if !ok {
		return false
	}
	plugins, ok := expo["plugins"].([]any)
	if !ok {
		return false
	}
	legacy := "./" + strings.TrimSuffix(PluginPath, ".js")
	for _, plugin := range plugins {
		switch value := plugin.(type) {
		case string:
			if value == legacy {
				return true
			}
		case []any:
			if len(value) > 0 && value[0] == legacy {
				return true
			}
		}
	}
	return false
}

func conflictingAttributionPluginRegistered(appJSON map[string]any) bool {
	expo, ok := appJSON["expo"].(map[string]any)
	if !ok {
		return false
	}
	plugins, ok := expo["plugins"].([]any)
	if !ok {
		return false
	}
	wanted := "./" + PluginPath
	for index, plugin := range plugins {
		name, ok := pluginRegistrationName(plugin)
		if !ok || !isAttributionPluginRegistration(name) {
			continue
		}
		if name != wanted || index != len(plugins)-1 {
			return true
		}
		if _, tuple := plugin.([]any); tuple {
			return true
		}
	}
	return false
}

func pluginRegistrationName(plugin any) (string, bool) {
	switch value := plugin.(type) {
	case string:
		return value, true
	case []any:
		if len(value) > 0 {
			name, ok := value[0].(string)
			return name, ok
		}
	}
	return "", false
}

func isAttributionPluginRegistration(name string) bool {
	return name == "./"+PluginPath ||
		name == "./"+strings.TrimSuffix(PluginPath, ".js") ||
		name == AttributionPkg ||
		name == AttributionEntry ||
		name == strings.TrimSuffix(AttributionEntry, ".js")
}

func appJSONWithPlugin(current map[string]any) (map[string]any, error) {
	// JSON round-tripping is an uncomplicated deep copy that keeps this
	// planner pure: callers' observed app.json object is never mutated.
	raw, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	next, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	expo := next["expo"].(map[string]any)
	plugins, _ := expo["plugins"].([]any)
	wanted := "./" + PluginPath
	cleaned := make([]any, 0, len(plugins)+1)
	for _, plugin := range plugins {
		if name, ok := pluginRegistrationName(plugin); ok && isAttributionPluginRegistration(name) {
			continue
		}
		cleaned = append(cleaned, plugin)
	}
	// The generated wrapper owns the final write for settings such as Meta
	// conversion reporting, so it must run after every other config plugin.
	cleaned = append(cleaned, wanted)
	expo["plugins"] = cleaned
	return next, nil
}
