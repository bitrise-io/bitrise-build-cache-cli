package daemon

const LabelPrefix = "io.bitrise.build-cache."

const UnitPrefix = "bitrise-build-cache-"

const (
	ServiceXcelerateProxy = "xcelerate-proxy"
	ServiceCcacheHelper   = "ccache-helper"
)

type Service struct {
	Name string
	Args []string
}

func (s Service) Label() string {
	return LabelPrefix + s.Name
}

func (s Service) UnitName() string {
	return UnitPrefix + s.Name
}

func DefaultServices() []Service {
	return []Service{
		{
			Name: ServiceXcelerateProxy,
			Args: []string{"xcelerate", "start-proxy"},
		},
		{
			Name: ServiceCcacheHelper,
			Args: []string{"ccache", "storage-helper", "start"},
		},
	}
}
