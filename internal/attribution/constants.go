package attribution

const (
	SchemaVersion    = "1.0.0"
	ConfigPath       = ".attribution/config.yaml"
	PluginPath       = ".attribution/plugin/withAttribution.js"
	ManifestPath     = ".attribution/manifest.json"
	LastRunPath      = ".attribution/last-run.json"
	AttributionPkg   = "@attributionkit/expo"
	AttributionEntry = "@attributionkit/expo/app.plugin.js"
	ReleaseBaseURL   = "https://github.com/attributionkit/attribution/releases/download"
)

// Version is a variable so release builds can embed the signed tag without
// modifying source: -ldflags "-X github.com/attributionkit/attribution/internal/attribution.Version=vX.Y.Z".
var Version = "0.1.0-preview.1"

var knownEvents = map[string]struct{}{
	"install":   {},
	"trial":     {},
	"purchase":  {},
	"retention": {},
}

type conversionManager struct {
	Package string
	Name    string
	Aliases []string
	// Disableable managers can remain ordinary event transports in managed
	// mode because the public Expo plugin turns off their conversion reporting.
	Disableable bool
}

var knownManagers = []conversionManager{
	{Package: "react-native-appsflyer", Name: "AppsFlyer", Aliases: []string{"appsflyer"}},
	{Package: "react-native-adjust", Name: "Adjust", Aliases: []string{"adjust"}},
	{Package: "react-native-branch", Name: "Branch", Aliases: []string{"branch"}},
	{Package: "@react-native-firebase/analytics", Name: "Firebase Analytics (GA SDK)", Aliases: []string{"firebase", "firebaseanalytics", "gasdk"}},
	{Package: "react-native-google-analytics", Name: "Google Analytics SDK", Aliases: []string{"googleanalytics", "googleanalyticssdk"}},
	{Package: "singular-react-native", Name: "Singular", Aliases: []string{"singular"}},
	{Package: "react-native-kochava-tracker", Name: "Kochava", Aliases: []string{"kochava"}},
	{Package: "react-native-tenjin", Name: "Tenjin", Aliases: []string{"tenjin"}},
	{Package: "react-native-fbsdk-next", Name: "Meta (Facebook SDK)", Aliases: []string{"meta", "facebook", "facebooksdk", "fbsdk"}, Disableable: true},
}
