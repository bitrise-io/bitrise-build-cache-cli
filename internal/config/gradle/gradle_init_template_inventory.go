package gradleconfig

type UsageLevel string

//nolint:gochecknoglobals
var (
	UsageLevelNone       UsageLevel = "none"
	UsageLevelDependency UsageLevel = "dependency"
	UsageLevelEnabled    UsageLevel = "enabled"
)

type CacheTemplateInventory struct {
	Usage               UsageLevel
	Version             string
	EndpointURLWithPort string
	IsPushEnabled       bool
	ValidationLevel     string
}

type AnalyticsTemplateInventory struct {
	Usage        UsageLevel
	Version      string
	Endpoint     string
	Port         int
	HTTPEndpoint string
}

type TestDistroTemplateInventory struct {
	Usage      UsageLevel
	Version    string
	Endpoint   string
	KvEndpoint string
	Port       int
	LogLevel   string
}

type PluginCommonTemplateInventory struct {
	AuthToken  string
	Debug      bool
	AppSlug    string
	CIProvider string
	Version    string
}

type TemplateInventory struct {
	Common     PluginCommonTemplateInventory
	Cache      CacheTemplateInventory
	Analytics  AnalyticsTemplateInventory
	TestDistro TestDistroTemplateInventory
}

func (inventory TemplateInventory) HasDependencies() bool {
	if inventory.Analytics.Usage == UsageLevelDependency || inventory.Analytics.Usage == UsageLevelEnabled {
		return true
	}
	if inventory.Cache.Usage == UsageLevelDependency || inventory.Cache.Usage == UsageLevelEnabled {
		return true
	}
	if inventory.TestDistro.Usage == UsageLevelDependency || inventory.TestDistro.Usage == UsageLevelEnabled {
		return true
	}

	return false
}
